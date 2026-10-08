-- property_runtime.lua
-- Hand-maintained pure Lua replay engine (extensible).
-- Generated *_sync.lua modules supply SCHEMA / field metadata only.
-- See docs/lua-sync.md

local Runtime = {}

local CMD = {
  clear = 0,
  set = 1,
  add = 2,
  erase = 3,
  push = 4,
  pop = 5,
  pop_erase = 6,
  item_change = 7,
  slot_swap = 8,
  slot_resize = 9,
  slot_move = 10,
  update_fields = 11,
}
Runtime.CMD = CMD

-- Flags that participate in observer / client sync (annotation names).
local SYNC_FLAG_SET = {
  sync_clients = true,
  sync_self = true,
  sync_other = true,
  sync_ghost = true,
}

--- Convert property_record_offset uint64 → 0-based field path (top first).
--- Mirrors property_record_offset::to_replay_offset + successive split.
function Runtime.record_offset_to_path(record_u64)
  local parts = {}
  local temp = record_u64
  while temp ~= 0 do
    local cur = temp % 256
    temp = (temp - cur) / 256
    assert(cur >= 1, "invalid record offset byte")
    parts[#parts + 1] = cur - 1
  end
  local path = {}
  for i = #parts, 1, -1 do
    path[#path + 1] = parts[i]
  end
  return path
end

--- Convert property_replay_offset uint64 → path (low byte = top index).
function Runtime.replay_offset_to_path(replay_u64)
  local path = {}
  local temp = replay_u64
  if temp == 0 then
    return { 0 }
  end
  local parts = {}
  while temp ~= 0 do
    local cur = temp % 256
    temp = (temp - cur) / 256
    parts[#parts + 1] = cur
  end
  for i = #parts, 1, -1 do
    path[#path + 1] = parts[i]
  end
  return path
end

function Runtime.resolve_path(msg)
  if type(msg.offset) == "table" then
    return msg.offset
  end
  if msg.offset_is_record then
    return Runtime.record_offset_to_path(msg.offset)
  end
  -- Host / exporter default: replay offset (root single-field → value == field index).
  return Runtime.replay_offset_to_path(msg.offset)
end

local function default_for_kind(kind)
  if kind == "bag" then
    return { items = {}, id_to_idx = {} }
  elseif kind == "slots" then
    return { size = 0, by_slot = {}, by_id = {} }
  elseif kind == "vec" or kind == "vector" or kind == "array" then
    return {}
  elseif kind == "map" then
    return {}
  elseif kind == "string" then
    return ""
  elseif kind == "number" then
    return 0
  elseif kind == "bool" then
    return false
  elseif kind == "object" then
    return {}
  end
  return nil
end

function Runtime.new_default(meta)
  local t = {}
  for _, f in ipairs(meta.fields) do
    t[f.name] = default_for_kind(f.wire_kind)
  end
  return t
end

local function bag_rebuild_index(bag)
  bag.id_to_idx = {}
  for i, it in ipairs(bag.items) do
    if it.id ~= nil then
      bag.id_to_idx[it.id] = i
    end
  end
end

local function field_name_by_index(item_meta, field_index)
  -- bag/slot base: id@0, optional slot@1; vec items have no id — index 0 is first field.
  if item_meta then
    if item_meta.has_bag_id and field_index == 0 then
      return "id"
    end
    if item_meta.has_slot and field_index == 1 then
      return "slot"
    end
    for _, f in ipairs(item_meta.fields) do
      if f.index == field_index then
        return f.name
      end
    end
  elseif field_index == 0 then
    return "id"
  end
  return "_" .. tostring(field_index)
end

--- Decode item snapshot: object form or [[field_index, value], ...]
local function decode_item_pairs(data, item_meta)
  local item = {}
  if type(data) ~= "table" then
    return item
  end
  -- object form (name keys)
  if data.id ~= nil or (next(data) ~= nil and data[1] == nil) then
    for k, v in pairs(data) do
      if type(k) == "string" then
        item[k] = v
      end
    end
    if next(item) ~= nil then
      return item
    end
  end
  for _, pair in ipairs(data) do
    if type(pair) == "table" and pair[1] ~= nil then
      item[field_name_by_index(item_meta, pair[1])] = pair[2]
    end
  end
  return item
end

local function apply_item_field(item, item_meta, field_path, cmd, value)
  local field_index = field_path[1]
  if field_index == nil then
    return false, "empty item field path"
  end
  local name = field_name_by_index(item_meta, field_index)
  if cmd == CMD.set then
    item[name] = value
    return true
  elseif cmd == CMD.clear then
    item[name] = nil
    return true
  end
  return false, "unsupported item field cmd " .. tostring(cmd)
end

local function bag_add(bag, data, item_meta)
  local item = decode_item_pairs(data, item_meta)
  bag.items[#bag.items + 1] = item
  bag_rebuild_index(bag)
  return true
end

local function bag_erase(bag, key)
  local idx = bag.id_to_idx[key]
  if not idx then
    return false, "bag erase missing id " .. tostring(key)
  end
  table.remove(bag.items, idx)
  bag_rebuild_index(bag)
  return true
end

local function bag_item_change(bag, data, item_meta)
  -- data = { item_idx, record_offset, cmd, payload }  (0-based dense idx)
  local item_idx = data[1]
  local record_off = data[2]
  local cmd = data[3]
  local payload = data[4]
  local item = bag.items[item_idx + 1]
  if not item then
    return false, "bag item_change bad idx " .. tostring(item_idx)
  end
  local path = Runtime.record_offset_to_path(record_off)
  local ok, err = apply_item_field(item, item_meta, path, cmd, payload)
  if ok then
    bag_rebuild_index(bag)
  end
  return ok, err
end

local function vector_apply(vec, cmd, data)
  if cmd == CMD.set then
    for i = #vec, 1, -1 do
      vec[i] = nil
    end
    if type(data) == "table" then
      for i, v in ipairs(data) do
        vec[i] = v
      end
    end
    return true
  elseif cmd == CMD.clear then
    for i = #vec, 1, -1 do
      vec[i] = nil
    end
    return true
  elseif cmd == CMD.push then
    vec[#vec + 1] = data
    return true
  elseif cmd == CMD.pop then
    vec[#vec] = nil
    return true
  elseif cmd == CMD.item_change then
    local idx, value = data[1], data[2]
    vec[idx + 1] = value
    return true
  elseif cmd == CMD.add then
    local idx, value = data[1], data[2]
    table.insert(vec, idx + 1, value)
    return true
  elseif cmd == CMD.erase then
    if type(data) == "number" then
      table.remove(vec, data + 1)
      return true
    elseif type(data) == "table" then
      local idx, num = data[1], data[2]
      for _ = 1, (num or 1) do
        table.remove(vec, idx + 1)
      end
      return true
    end
  end
  return false, "vector cmd " .. tostring(cmd)
end

local function map_apply(map, cmd, data)
  if cmd == CMD.set then
    for k in pairs(map) do
      map[k] = nil
    end
    if type(data) ~= "table" then
      return true
    end
    -- list of pairs
    if data[1] and type(data[1]) == "table" and data[1][1] ~= nil then
      for _, pair in ipairs(data) do
        map[pair[1]] = pair[2]
      end
      return true
    end
    for k, v in pairs(data) do
      map[k] = v
    end
    return true
  elseif cmd == CMD.clear then
    for k in pairs(map) do
      map[k] = nil
    end
    return true
  elseif cmd == CMD.add then
    if type(data) == "table" then
      map[data[1]] = data[2]
      return true
    end
  elseif cmd == CMD.erase then
    map[data] = nil
    return true
  end
  return false, "map cmd " .. tostring(cmd)
end

local function slots_rebuild_id(slots)
  slots.by_id = {}
  for _, it in pairs(slots.by_slot) do
    if it and it.id ~= nil then
      slots.by_id[it.id] = it
    end
  end
end

local function slots_resize(slots, new_sz)
  slots.size = new_sz
  -- drop items whose slot is out of range
  for slot, it in pairs(slots.by_slot) do
    if slot >= new_sz then
      slots.by_slot[slot] = nil
    end
  end
  slots_rebuild_id(slots)
  return true
end

local function slots_add(slots, data, item_meta)
  local item = decode_item_pairs(data, item_meta)
  local slot = item.slot
  if slot == nil then
    return false, "slots add missing slot"
  end
  if slots.by_slot[slot] ~= nil then
    return false, "slots add occupied " .. tostring(slot)
  end
  slots.by_slot[slot] = item
  if item.id ~= nil then
    slots.by_id[item.id] = item
  end
  return true
end

local function slots_swap(slots, a, b)
  local ia, ib = slots.by_slot[a], slots.by_slot[b]
  slots.by_slot[a], slots.by_slot[b] = ib, ia
  if ia then
    ia.slot = b
  end
  if ib then
    ib.slot = a
  end
  return true
end

local function slots_move(slots, from, to)
  local it = slots.by_slot[from]
  if not it then
    return false, "slots move empty from"
  end
  if slots.by_slot[to] ~= nil then
    return false, "slots move dest occupied"
  end
  slots.by_slot[from] = nil
  slots.by_slot[to] = it
  it.slot = to
  return true
end

--- slots erase data = slot index (not id)
local function slots_erase_slot(slots, slot)
  local it = slots.by_slot[slot]
  if not it then
    return false, "slots erase empty slot " .. tostring(slot)
  end
  slots.by_slot[slot] = nil
  if it.id ~= nil then
    slots.by_id[it.id] = nil
  end
  return true
end

--- item_change first element is SLOT index for property_slots
local function slots_item_change(slots, data, item_meta)
  local slot = data[1]
  local record_off = data[2]
  local cmd = data[3]
  local payload = data[4]
  local item = slots.by_slot[slot]
  if not item then
    return false, "slots item_change empty slot " .. tostring(slot)
  end
  local path = Runtime.record_offset_to_path(record_off)
  local ok, err = apply_item_field(item, item_meta, path, cmd, payload)
  if ok and (path[1] == 0 or path[1] == 1) then
    slots_rebuild_id(slots)
  end
  return ok, err
end

local function vec_item_change(vec, data, item_meta)
  local item_idx = data[1]
  local record_off = data[2]
  local cmd = data[3]
  local payload = data[4]
  local item = vec[item_idx + 1]
  if type(item) ~= "table" then
    item = {}
    vec[item_idx + 1] = item
  end
  local path = Runtime.record_offset_to_path(record_off)
  return apply_item_field(item, item_meta, path, cmd, payload)
end

local function vec_push_item(vec, data, item_meta)
  vec[#vec + 1] = decode_item_pairs(data, item_meta)
  return true
end

--- Apply mutate to a root object using class meta module.
function Runtime.apply_mutate(obj, msg, meta)
  assert(obj and msg and meta, "apply_mutate args")
  local path = Runtime.resolve_path(msg)
  local top = path[1]
  local field = meta.by_index[top]
  if not field then
    return false, "unknown field index " .. tostring(top)
  end
  local cmd = msg.cmd
  local data = msg.data
  local kind = field.wire_kind
  local slot = obj[field.name]

  if kind == "number" or kind == "string" or kind == "bool" or kind == "object" or kind == "other" then
    if cmd == CMD.set then
      obj[field.name] = data
      return true
    elseif cmd == CMD.clear then
      obj[field.name] = default_for_kind(kind)
      return true
    end
  elseif kind == "vector" or kind == "array" then
    return vector_apply(slot, cmd, data)
  elseif kind == "map" then
    return map_apply(slot, cmd, data)
  elseif kind == "bag" then
    if cmd == CMD.set then
      obj[field.name] = { items = {}, id_to_idx = {} }
      if type(data) == "table" then
        for _, row in ipairs(data) do
          bag_add(obj[field.name], row, field.item_meta)
        end
      end
      return true
    elseif cmd == CMD.clear then
      obj[field.name] = { items = {}, id_to_idx = {} }
      return true
    elseif cmd == CMD.add then
      return bag_add(slot, data, field.item_meta)
    elseif cmd == CMD.erase then
      return bag_erase(slot, data)
    elseif cmd == CMD.item_change then
      return bag_item_change(slot, data, field.item_meta)
    end
  elseif kind == "slots" then
    if cmd == CMD.clear then
      obj[field.name] = default_for_kind("slots")
      return true
    elseif cmd == CMD.set then
      local s = default_for_kind("slots")
      if type(data) == "table" then
        s.size = data.sz or data.size or 0
        local rows = data.data or data
        if type(rows) == "table" then
          for _, row in ipairs(rows) do
            slots_add(s, row, field.item_meta)
          end
        end
      end
      obj[field.name] = s
      return true
    elseif cmd == CMD.slot_resize then
      return slots_resize(slot, data)
    elseif cmd == CMD.add then
      return slots_add(slot, data, field.item_meta)
    elseif cmd == CMD.erase then
      return slots_erase_slot(slot, data)
    elseif cmd == CMD.slot_swap then
      return slots_swap(slot, data[1], data[2])
    elseif cmd == CMD.slot_move then
      return slots_move(slot, data[1], data[2])
    elseif cmd == CMD.item_change then
      return slots_item_change(slot, data, field.item_meta)
    end
  elseif kind == "vec" then
    if cmd == CMD.set then
      obj[field.name] = {}
      if type(data) == "table" then
        for _, row in ipairs(data) do
          vec_push_item(obj[field.name], row, field.item_meta)
        end
      end
      return true
    elseif cmd == CMD.clear then
      obj[field.name] = {}
      return true
    elseif cmd == CMD.push then
      return vec_push_item(slot, data, field.item_meta)
    elseif cmd == CMD.pop then
      slot[#slot] = nil
      return true
    elseif cmd == CMD.add then
      local idx, row = data[1], data[2]
      table.insert(slot, idx + 1, decode_item_pairs(row, field.item_meta))
      return true
    elseif cmd == CMD.erase then
      if type(data) == "number" then
        table.remove(slot, data + 1)
        return true
      elseif type(data) == "table" then
        local idx, num = data[1], data[2]
        for _ = 1, (num or 1) do
          table.remove(slot, idx + 1)
        end
        return true
      end
    elseif cmd == CMD.item_change then
      return vec_item_change(slot, data, field.item_meta)
    end
  end
  return false, string.format("unsupported kind=%s cmd=%s field=%s", kind, tostring(cmd), field.name)
end

local function copy_map(src)
  local m = {}
  if type(src) == "table" then
    for k, v in pairs(src) do
      m[k] = v
    end
  end
  return m
end

local function copy_array(src)
  local a = {}
  if type(src) == "table" then
    for i, v in ipairs(src) do
      a[i] = v
    end
  end
  return a
end

--- Replace obj contents from a sync/snapshot JSON object (name keys).
--- Resets all fields to defaults first so omitted keys clear state.
function Runtime.load_snapshot(obj, snap, meta)
  assert(obj and snap and meta, "load_snapshot args")
  if snap.schema_version ~= nil and meta.SCHEMA_VERSION ~= nil
      and snap.schema_version ~= meta.SCHEMA_VERSION then
    error(string.format(
      "schema mismatch: packet=%s local=%s (no hot-reload)",
      tostring(snap.schema_version), tostring(meta.SCHEMA_VERSION)))
  end
  for _, f in ipairs(meta.fields) do
    obj[f.name] = default_for_kind(f.wire_kind)
  end
  for _, f in ipairs(meta.fields) do
    if snap[f.name] ~= nil then
      local kind = f.wire_kind
      local raw = snap[f.name]
      if kind == "bag" and type(raw) == "table" then
        local bag = { items = {}, id_to_idx = {} }
        for _, row in ipairs(raw) do
          bag_add(bag, row, f.item_meta)
        end
        obj[f.name] = bag
      elseif kind == "slots" and type(raw) == "table" then
        local s = default_for_kind("slots")
        s.size = raw.sz or raw.size or 0
        local rows = raw.data
        if type(rows) == "table" then
          for _, row in ipairs(rows) do
            slots_add(s, row, f.item_meta)
          end
        end
        obj[f.name] = s
      elseif kind == "vec" and type(raw) == "table" then
        local vec = {}
        for _, row in ipairs(raw) do
          vec_push_item(vec, row, f.item_meta)
        end
        obj[f.name] = vec
      elseif kind == "map" then
        obj[f.name] = copy_map(raw)
      elseif kind == "vector" or kind == "array" then
        obj[f.name] = copy_array(raw)
      else
        obj[f.name] = raw
      end
    end
  end
  return obj
end

-- ---------- encode_sync_view (C++ encode_with_flag mirror) ----------

local function flags_match_sync(flags)
  if not flags or #flags == 0 then
    return true
  end
  for _, name in ipairs(flags) do
    if SYNC_FLAG_SET[name] then
      return true
    end
  end
  return false
end

local function is_default_scalar(v)
  return v == nil or v == 0 or v == "" or v == false
end

local function encode_item_object(item, item_meta, ignore_default)
  local out = {}
  if item_meta and item_meta.has_bag_id and item.id ~= nil then
    out.id = item.id
  end
  if item_meta and item_meta.has_slot and item.slot ~= nil then
    out.slot = item.slot
  end
  if item_meta then
    for _, f in ipairs(item_meta.fields) do
      if flags_match_sync(f.flags) then
        local v = item[f.name]
        if v ~= nil and (not ignore_default or not is_default_scalar(v)) then
          out[f.name] = v
        end
      end
    end
  else
    for k, v in pairs(item) do
      if (not ignore_default or not is_default_scalar(v)) then
        out[k] = v
      end
    end
  end
  return out
end

local function encode_field_value(value, field, ignore_default)
  local kind = field.wire_kind
  if kind == "bag" then
    local arr = {}
    for _, it in ipairs(value.items or {}) do
      arr[#arr + 1] = encode_item_object(it, field.item_meta, ignore_default)
    end
    return arr
  elseif kind == "slots" then
    local data = {}
    local max_slot = -1
    for slot, _ in pairs(value.by_slot or {}) do
      if slot > max_slot then
        max_slot = slot
      end
    end
    for slot = 0, math.max(max_slot, (value.size or 0) - 1) do
      local it = value.by_slot[slot]
      if it then
        data[#data + 1] = encode_item_object(it, field.item_meta, ignore_default)
      end
    end
    return { sz = value.size or 0, data = data }
  elseif kind == "vec" then
    local arr = {}
    for _, it in ipairs(value) do
      arr[#arr + 1] = encode_item_object(it, field.item_meta, ignore_default)
    end
    return arr
  elseif kind == "map" then
    local out = {}
    for k, v in pairs(value) do
      out[k] = v
    end
    return out
  elseif kind == "vector" or kind == "array" then
    local arr = {}
    for i, v in ipairs(value) do
      arr[i] = v
    end
    return arr
  else
    return value
  end
end

--- Mirror C++ encode_with_flag(sync_clients, ignore_default=true, replace_key_by_index=false)
function Runtime.encode_sync_view(obj, meta, opts)
  opts = opts or {}
  local ignore_default = opts.ignore_default
  if ignore_default == nil then
    ignore_default = true
  end
  local out = {}
  for _, f in ipairs(meta.fields) do
    if flags_match_sync(f.flags) then
      local v = obj[f.name]
      if v ~= nil then
        local encoded = encode_field_value(v, f, ignore_default)
        local skip = false
        if ignore_default then
          if f.wire_kind == "number" or f.wire_kind == "string" or f.wire_kind == "bool" then
            skip = is_default_scalar(encoded)
          elseif f.wire_kind == "map" then
            skip = next(encoded) == nil
          elseif f.wire_kind == "vector" or f.wire_kind == "array" or f.wire_kind == "vec" or f.wire_kind == "bag" then
            skip = #encoded == 0
          end
        end
        if not skip then
          out[f.name] = encoded
        end
      end
    end
  end
  return out
end

return Runtime
