-- property_record.lua
-- Pure-Lua Record (W2): mutate 源表 + enqueue mutate_msg (C++-shaped).
--
-- Model:
--   源表 obj     = plain Lua table (authoritative memory shape)
--   meta         = *_meta.lua (fields / wire_kind / flags / INDEX)
--   Record facade = metatable over (obj, meta): assign + container proxies
--
-- Preferred call style (源表 + meta + 元表门面):
--   local data = PlayerMeta.new_default()
--   local rec = Record.bind(PlayerMeta, { obj = data, flags = FLAGS })
--   rec.hp = 80
--   rec.tags:push("vip")
--   rec.attrs.atk = 100
--   rec.inventory:insert({ id = 1001, count = 1 })
--   rec.inventory[1001].count = 5           -- ItemProxy
--   local it = rec.inventory:get_insert(3001)
--   it.count = 2
--   rec.equipment[0].enhance = 3
--   rec.login_history[1].logout_ts = 400
--   -- rec.hp = nil / rec.unknown = 1       -- error
--   local batch = rec:drain()
--
-- Low-level string APIs (rec:set / bag_item_set / …) remain for tooling.
-- See docs/lua-record.md.

local Runtime = require("property_runtime")

local Record = {}
-- NOTE: instances use custom __index / __newindex; do not set Record.__index.

local CMD = Runtime.CMD

-- STL 序列：整表 set / push·pop·erase（array 无 push，改用 item_change）
local function is_seq_kind(kind)
  return kind == "vector" or kind == "list" or kind == "array"
end

-- STL 字典：整表 set / 子键赋值 / insert·erase
local function is_dict_kind(kind)
  return kind == "map" or kind == "dict"
end

-- 标量：门面 __newindex 直接 commit(set)；__index 返回源表值（无代理）
local function is_scalar_kind(kind)
  return kind == "number" or kind == "string" or kind == "bool" or kind == "object" or kind == "other"
end

-- 复杂容器：禁止整表赋值；经 FieldProxy / ItemProxy 操作
local function is_complex_container(kind)
  return kind == "bag" or kind == "slots" or kind == "vec"
end

-- encode_item_pairs：跳过默认标量，减少 sync 载荷
local function is_default_scalar(v)
  return v == nil or v == 0 or v == "" or v == false
end

-- 在 item_meta.fields 里按名查找子字段（供 ItemProxy / item_set）
local function find_item_field(item_meta, field_name)
  if not item_meta then
    return nil
  end
  for _, f in ipairs(item_meta.fields or {}) do
    if f.name == field_name then
      return f
    end
  end
  return nil
end

--- Single-field record_offset (C++ merge from empty parent): index + 1
local function item_field_record_offset(field_index)
  return field_index + 1
end

--- Resolve alias/bit names → uint64 mask (1<<bit).
local function resolve_mask(names, flags_def)
  if not names or #names == 0 then
    return 0
  end
  flags_def = flags_def or {}
  local bits = flags_def.bits or {}
  local aliases = flags_def.aliases or {}
  local mask = 0
  local function add_name(name)
    if name == "*" then
      -- all bits: approximate with max uint53 safe for Lua numbers
      mask = 0x1fffffffffffff
      return
    end
    if bits[name] ~= nil then
      mask = mask | (1 << bits[name])
      return
    end
    local alias = aliases[name]
    if alias then
      if alias[1] == "*" then
        mask = 0x1fffffffffffff
        return
      end
      for _, part in ipairs(alias) do
        add_name(part)
      end
      return
    end
    error("unknown flag name: " .. tostring(name))
  end
  for _, n in ipairs(names) do
    add_name(n)
  end
  return mask
end

-- C++ include_by: (need & data) == need
local function include_by(need_mask, data_mask)
  return (need_mask & data_mask) == need_mask
end

local function check_scalar_value(kind, value, name)
  if kind == "number" and type(value) ~= "number" then
    error("field " .. name .. " expects number, got " .. type(value))
  elseif kind == "string" and type(value) ~= "string" then
    error("field " .. name .. " expects string, got " .. type(value))
  elseif kind == "bool" and type(value) ~= "boolean" then
    error("field " .. name .. " expects boolean, got " .. type(value))
  end
end

--- Resolve raw item table + wire locator for bag/slots/vec.
--- Returns nil, nil if missing.
local function resolve_raw_item(rec, name, kind, locator)
  local container = rec.obj[name]
  if not container then
    return nil, nil
  end
  if kind == "bag" then
    local lua_idx = container.id_to_idx[locator]
    if not lua_idx then
      return nil, nil
    end
    return container.items[lua_idx], lua_idx - 1
  elseif kind == "slots" then
    local it = container.by_slot[locator]
    if not it then
      return nil, nil
    end
    return it, locator
  elseif kind == "vec" then
    local it = container[locator + 1]
    if not it then
      return nil, nil
    end
    return it, locator
  end
  return nil, nil
end

--- ItemProxy: bag[id] / slots[slot] / vec[idx] → field assign enqueues item_change.
local ItemProxy = {}

local function item_proxy_index(self, key)
  local method = ItemProxy[key]
  if method ~= nil then
    return method
  end
  local raw = resolve_raw_item(self._rec, self._name, self._kind, self._loc)
  if not raw then
    return nil
  end
  return raw[key]
end

local function item_proxy_newindex(self, key, value)
  if type(key) ~= "string" then
    error("item field name must be a string")
  end
  if value == nil then
    error("cannot delete item field: " .. key)
  end
  if key == "id" or key == "slot" then
    error("cannot assign structural field: " .. key)
  end
  local f = self._rec:field(self._name)
  local item_field = find_item_field(f.item_meta, key)
  if not item_field then
    error("unknown item field: " .. key)
  end
  check_scalar_value(item_field.wire_kind, value, key)
  local kind = self._kind
  if kind == "bag" then
    return self._rec:bag_item_set(self._name, self._loc, key, value)
  elseif kind == "slots" then
    return self._rec:slots_item_set(self._name, self._loc, key, value)
  elseif kind == "vec" then
    return self._rec:vec_item_set(self._name, self._loc, key, value)
  end
  error("item assign unsupported for " .. tostring(kind))
end

local function make_item_proxy(rec, name, kind, locator)
  local raw = resolve_raw_item(rec, name, kind, locator)
  if not raw then
    return nil
  end
  return setmetatable({
    _rec = rec,
    _name = name,
    _kind = kind,
    _loc = locator,
  }, {
    __index = item_proxy_index,
    __newindex = item_proxy_newindex,
  })
end

--- Container field proxy (array/vector/map/bag/slots/vec). Scalars are not proxied.
local FieldProxy = {}

local function field_proxy_index(self, key)
  local method = FieldProxy[key]
  if method ~= nil then
    return method
  end
  local kind = self._rec:field(self._name).wire_kind
  if is_dict_kind(kind) then
    local m = self._rec.obj[self._name]
    return m and m[key] or nil
  elseif kind == "bag" or kind == "slots" or kind == "vec" then
    return make_item_proxy(self._rec, self._name, kind, key)
  end
  return nil
end

local function field_proxy_newindex(self, key, value)
  local kind = self._rec:field(self._name).wire_kind
  if is_dict_kind(kind) then
    if value == nil then
      return self._rec:erase_key(self._name, key)
    end
    return self._rec:insert(self._name, key, value)
  end
  error("cannot assign into " .. tostring(kind) .. " proxy " .. self._name)
end

local function make_field_proxy(rec, name)
  return setmetatable({ _rec = rec, _name = name }, {
    __index = field_proxy_index,
    __newindex = field_proxy_newindex,
  })
end

function FieldProxy:get(locator)
  if locator == nil then
    return self._rec.obj[self._name]
  end
  local kind = self._rec:field(self._name).wire_kind
  if kind == "bag" or kind == "slots" or kind == "vec" then
    return make_item_proxy(self._rec, self._name, kind, locator)
  end
  error("get(locator): " .. self._name .. " is not bag/slots/vec")
end

function FieldProxy:set(value)
  local rec, name = self._rec, self._name
  local kind = rec:field(name).wire_kind
  if is_seq_kind(kind) then
    return rec:seq_set(name, value)
  elseif is_dict_kind(kind) then
    return rec:map_set(name, value)
  end
  error("set: unsupported wire_kind " .. tostring(kind) .. " for " .. name)
end

function FieldProxy:clear()
  return self._rec:clear(self._name)
end

function FieldProxy:item_change(idx, value)
  return self._rec:item_change(self._name, idx, value)
end

function FieldProxy:push(value)
  local kind = self._rec:field(self._name).wire_kind
  if kind == "vec" then
    return self._rec:vec_push(self._name, value)
  end
  return self._rec:push(self._name, value)
end

function FieldProxy:pop()
  local kind = self._rec:field(self._name).wire_kind
  if kind == "vec" then
    return self._rec:vec_pop(self._name)
  end
  return self._rec:pop(self._name)
end

function FieldProxy:add(idx, value)
  local kind = self._rec:field(self._name).wire_kind
  if kind == "vec" then
    return self._rec:vec_insert(self._name, idx, value)
  end
  return self._rec:seq_add(self._name, idx, value)
end

function FieldProxy:erase(a, b)
  local kind = self._rec:field(self._name).wire_kind
  if is_dict_kind(kind) then
    return self._rec:erase_key(self._name, a)
  elseif kind == "bag" then
    return self._rec:bag_erase(self._name, a)
  elseif kind == "slots" then
    return self._rec:slots_erase_slot(self._name, a)
  elseif kind == "vec" then
    return self._rec:vec_erase(self._name, a, b)
  elseif kind == "vector" or kind == "list" then
    return self._rec:seq_erase(self._name, a, b)
  end
  error("erase: unsupported for " .. tostring(kind))
end

function FieldProxy:insert(a, b)
  local kind = self._rec:field(self._name).wire_kind
  if is_dict_kind(kind) then
    return self._rec:insert(self._name, a, b)
  elseif kind == "bag" then
    return self._rec:bag_insert(self._name, a)
  elseif kind == "slots" then
    return self._rec:slots_insert(self._name, a)
  elseif kind == "vec" then
    return self._rec:vec_insert(self._name, a, b)
  end
  error("insert: unsupported for " .. tostring(kind))
end

--- Ensure bag item exists; returns ItemProxy (never raw table).
function FieldProxy:get_insert(id)
  self._rec:bag_get_insert(self._name, id)
  return make_item_proxy(self._rec, self._name, "bag", id)
end

function FieldProxy:item_set(locator, field_name, value)
  local kind = self._rec:field(self._name).wire_kind
  if kind == "bag" then
    return self._rec:bag_item_set(self._name, locator, field_name, value)
  elseif kind == "slots" then
    return self._rec:slots_item_set(self._name, locator, field_name, value)
  elseif kind == "vec" then
    return self._rec:vec_item_set(self._name, locator, field_name, value)
  end
  error("item_set: unsupported for " .. tostring(kind))
end

function FieldProxy:resize(size)
  return self._rec:slots_resize(self._name, size)
end

function FieldProxy:swap(a, b)
  return self._rec:slots_swap(self._name, a, b)
end

function FieldProxy:move(frm, to)
  return self._rec:slots_move(self._name, frm, to)
end

function FieldProxy:erase_id(id)
  return self._rec:slots_erase_id(self._name, id)
end

function FieldProxy:first_empty()
  return self._rec:slots_first_empty(self._name)
end

--- Facade __index: Record methods, scalar values, or container proxies.
local function record_index(self, key)
  local method = Record[key]
  if method ~= nil then
    return method
  end
  local idx = self.sync.INDEX and self.sync.INDEX[key]
  if idx == nil then
    return nil
  end
  local f = self.sync.by_index[idx]
  local kind = f.wire_kind
  if is_scalar_kind(kind) then
    return self.obj[key]
  end
  local cache = self._proxies
  local p = cache[key]
  if not p then
    p = make_field_proxy(self, key)
    cache[key] = p
  end
  return p
end

--- Facade __newindex: type-dispatch assign; reject nil (防删) and unknown keys.
local function record_newindex(self, key, value)
  if type(key) ~= "string" then
    error("record field name must be a string")
  end
  local idx = self.sync.INDEX and self.sync.INDEX[key]
  if idx == nil then
    error("unknown field: " .. key)
  end
  if value == nil then
    error("cannot delete field: " .. key .. " (use rec:clear(\"" .. key .. "\"))")
  end
  local f = self.sync.by_index[idx]
  local kind = f.wire_kind
  if is_scalar_kind(kind) then
    check_scalar_value(kind, value, key)
    return self:set(key, value)
  elseif is_seq_kind(kind) then
    if type(value) ~= "table" then
      error("field " .. key .. " expects table sequence, got " .. type(value))
    end
    return self:seq_set(key, value)
  elseif is_dict_kind(kind) then
    if type(value) ~= "table" then
      error("field " .. key .. " expects table map, got " .. type(value))
    end
    return self:map_set(key, value)
  elseif is_complex_container(kind) then
    error("cannot assign whole " .. kind .. " field " .. key .. "; use proxy methods")
  end
  error("unsupported wire_kind " .. tostring(kind) .. " for " .. key)
end

--- Bind meta + 源表 into a Record facade.
--- opts.obj = existing 源表 (default: meta.new_default()).
function Record.bind(meta_mod, opts)
  opts = opts or {}
  assert(meta_mod and meta_mod.fields and meta_mod.by_index, "meta module required")
  local self = {
    sync = meta_mod,
    meta = meta_mod,
    flags_def = opts.flags or meta_mod.FLAGS or {},
    obj = opts.obj or meta_mod.new_default(),
    queue = {},
    _proxies = {},
  }
  local need_names = opts.need_flag_names or { "sync_clients" }
  if opts.need_masks then
    self.need_masks = opts.need_masks
  else
    self.need_masks = {}
    for _, n in ipairs(need_names) do
      self.need_masks[#self.need_masks + 1] = resolve_mask({ n }, self.flags_def)
    end
  end
  return setmetatable(self, { __index = record_index, __newindex = record_newindex })
end

--- Open over an existing 源表: Record.open(meta, obj, opts?).
function Record.open(meta_mod, obj, opts)
  opts = opts or {}
  opts.obj = obj
  return Record.bind(meta_mod, opts)
end

--- Access the underlying 源表 (plain data). Prefer writing via the facade.
function Record:data()
  return self.obj
end

function Record:field(name)
  local idx = self.sync.INDEX and self.sync.INDEX[name]
  if idx == nil then
    error("unknown field " .. tostring(name))
  end
  local f = self.sync.by_index[idx]
  if not f then
    error("no meta for field " .. tostring(name))
  end
  return f
end

function Record:field_mask(field)
  return resolve_mask(field.flags, self.flags_def)
end

function Record:is_flag_need(data_mask)
  for _, need in ipairs(self.need_masks) do
    if include_by(need, data_mask) then
      return true
    end
  end
  return false
end

function Record:enqueue(field, cmd, data, data_mask)
  data_mask = data_mask or self:field_mask(field)
  if not self:is_flag_need(data_mask) then
    return false
  end
  self.queue[#self.queue + 1] = {
    offset = field.index,
    offset_is_record = false,
    cmd = cmd,
    flag = data_mask,
    data = data,
  }
  return true
end

--- Apply locally (Replay) then optionally already enqueued separately.
function Record:apply_local(field, cmd, data)
  local msg = {
    offset = field.index,
    offset_is_record = false,
    cmd = cmd,
    data = data,
  }
  local ok, err = Runtime.apply_mutate(self.obj, msg, self.meta)
  if not ok then
    error(err or "apply_mutate failed")
  end
end

function Record:commit(field, cmd, data, data_mask)
  -- C++ order: mutate memory, then enqueue if needed
  self:apply_local(field, cmd, data)
  self:enqueue(field, cmd, data, data_mask)
end

--- Encode item as [[field_index, value], ...] filtered by need_flags (mirror encode_with_flag).
function Record:encode_item_pairs(item, item_meta)
  local pairs = {}
  if not item_meta or type(item) ~= "table" then
    return pairs
  end
  if item_meta.has_bag_id and item.id ~= nil then
    pairs[#pairs + 1] = { 0, item.id }
  end
  if item_meta.has_slot and item.slot ~= nil then
    pairs[#pairs + 1] = { 1, item.slot }
  end
  for _, f in ipairs(item_meta.fields or {}) do
    local v = item[f.name]
    if v ~= nil and not is_default_scalar(v) then
      local mask = resolve_mask(f.flags, self.flags_def)
      if self:is_flag_need(mask) then
        pairs[#pairs + 1] = { f.index, v }
      end
    end
  end
  return pairs
end

function Record:commit_item_change(container_field, locator, item_field, value)
  local record_off = item_field_record_offset(item_field.index)
  local data = { locator, record_off, CMD.set, value }
  local data_mask = resolve_mask(item_field.flags, self.flags_def)
  self:commit(container_field, CMD.item_change, data, data_mask)
end

function Record:drain()
  local q = self.queue
  rawset(self, "queue", {})
  return q
end

function Record:peek()
  return self.queue
end

function Record:view(opts)
  return Runtime.encode_sync_view(self.obj, self.meta, opts)
end

-- ---------- scalar ----------

function Record:set(name, value)
  local f = self:field(name)
  local kind = f.wire_kind
  if not is_scalar_kind(kind) then
    error("set: field " .. name .. " is " .. tostring(kind) .. " (use typed APIs)")
  end
  self:commit(f, CMD.set, value)
end

function Record:clear(name)
  local f = self:field(name)
  local kind = f.wire_kind
  if is_scalar_kind(kind) or is_seq_kind(kind) or is_dict_kind(kind)
      or kind == "bag" or kind == "slots" or kind == "vec" then
    self:commit(f, CMD.clear, nil)
    return
  end
  error("clear unsupported for " .. tostring(kind))
end

-- ---------- array / list / vector ----------

function Record:seq_set(name, arr)
  local f = self:field(name)
  if not is_seq_kind(f.wire_kind) then
    error("seq_set: " .. name .. " is not a sequence")
  end
  self:commit(f, CMD.set, arr)
end

function Record:push(name, value)
  local f = self:field(name)
  if f.wire_kind ~= "vector" and f.wire_kind ~= "list" then
    error("push: " .. name .. " must be list/vector")
  end
  self:commit(f, CMD.push, value)
end

function Record:pop(name)
  local f = self:field(name)
  if f.wire_kind ~= "vector" and f.wire_kind ~= "list" then
    error("pop: " .. name .. " must be list/vector")
  end
  self:commit(f, CMD.pop, nil)
end

function Record:item_change(name, idx, value)
  local f = self:field(name)
  if not is_seq_kind(f.wire_kind) then
    error("item_change: " .. name .. " must be array/list/vector")
  end
  self:commit(f, CMD.item_change, { idx, value })
end

function Record:seq_add(name, idx, value)
  local f = self:field(name)
  if f.wire_kind ~= "vector" and f.wire_kind ~= "list" then
    error("seq_add: " .. name .. " must be list/vector")
  end
  self:commit(f, CMD.add, { idx, value })
end

function Record:seq_erase(name, idx, count)
  local f = self:field(name)
  if f.wire_kind ~= "vector" and f.wire_kind ~= "list" then
    error("seq_erase: " .. name .. " must be list/vector")
  end
  if count and count > 1 then
    self:commit(f, CMD.erase, { idx, count })
  else
    self:commit(f, CMD.erase, idx)
  end
end

-- ---------- map / dict ----------

function Record:map_set(name, tbl)
  local f = self:field(name)
  if not is_dict_kind(f.wire_kind) then
    error("map_set: " .. name .. " must be map/dict")
  end
  self:commit(f, CMD.set, tbl)
end

function Record:insert(name, key, value)
  local f = self:field(name)
  if not is_dict_kind(f.wire_kind) then
    error("insert: " .. name .. " must be map/dict")
  end
  self:commit(f, CMD.add, { key, value })
end

function Record:erase_key(name, key)
  local f = self:field(name)
  if not is_dict_kind(f.wire_kind) then
    error("erase_key: " .. name .. " must be map/dict")
  end
  self:commit(f, CMD.erase, key)
end

-- ---------- bag ----------

function Record:bag_insert(name, item)
  local f = self:field(name)
  if f.wire_kind ~= "bag" then
    error("bag_insert: " .. name .. " must be bag")
  end
  local pairs = self:encode_item_pairs(item, f.item_meta)
  self:commit(f, CMD.add, pairs)
end

function Record:bag_erase(name, id)
  local f = self:field(name)
  if f.wire_kind ~= "bag" then
    error("bag_erase: " .. name .. " must be bag")
  end
  self:commit(f, CMD.erase, id)
end

function Record:bag_get_insert(name, id)
  local f = self:field(name)
  if f.wire_kind ~= "bag" then
    error("bag_get_insert: " .. name .. " must be bag")
  end
  local bag = self.obj[f.name]
  if bag.id_to_idx[id] then
    return bag.items[bag.id_to_idx[id]]
  end
  local item = { id = id }
  self:bag_insert(name, item)
  return bag.items[bag.id_to_idx[id]]
end

function Record:bag_item_set(name, id, field_name, value)
  local f = self:field(name)
  if f.wire_kind ~= "bag" then
    error("bag_item_set: " .. name .. " must be bag")
  end
  local bag = self.obj[f.name]
  local lua_idx = bag.id_to_idx[id]
  if not lua_idx then
    error("bag_item_set: missing id " .. tostring(id))
  end
  local item_field = find_item_field(f.item_meta, field_name)
  if not item_field then
    error("bag_item_set: unknown item field " .. tostring(field_name))
  end
  self:commit_item_change(f, lua_idx - 1, item_field, value)
end

-- ---------- slots ----------

function Record:slots_resize(name, size)
  local f = self:field(name)
  if f.wire_kind ~= "slots" then
    error("slots_resize: " .. name .. " must be slots")
  end
  self:commit(f, CMD.slot_resize, size)
end

--- Insert item; silent no-op (no enqueue) if slot out of capacity — mirrors C++.
function Record:slots_insert(name, item)
  local f = self:field(name)
  if f.wire_kind ~= "slots" then
    error("slots_insert: " .. name .. " must be slots")
  end
  local slots = self.obj[f.name]
  local slot = item.slot
  if slot == nil then
    error("slots_insert: item.slot required")
  end
  if slot < 0 or slot >= (slots.size or 0) then
    return false
  end
  if slots.by_slot[slot] ~= nil then
    return false
  end
  local pairs = self:encode_item_pairs(item, f.item_meta)
  self:commit(f, CMD.add, pairs)
  return true
end

function Record:slots_erase_slot(name, slot)
  local f = self:field(name)
  if f.wire_kind ~= "slots" then
    error("slots_erase_slot: " .. name .. " must be slots")
  end
  self:commit(f, CMD.erase, slot)
end

function Record:slots_erase_id(name, id)
  local f = self:field(name)
  if f.wire_kind ~= "slots" then
    error("slots_erase_id: " .. name .. " must be slots")
  end
  local slots = self.obj[f.name]
  local it = slots.by_id[id]
  if not it then
    error("slots_erase_id: missing id " .. tostring(id))
  end
  self:commit(f, CMD.erase, it.slot)
end

function Record:slots_swap(name, a, b)
  local f = self:field(name)
  if f.wire_kind ~= "slots" then
    error("slots_swap: " .. name .. " must be slots")
  end
  self:commit(f, CMD.slot_swap, { a, b })
end

function Record:slots_move(name, from, to)
  local f = self:field(name)
  if f.wire_kind ~= "slots" then
    error("slots_move: " .. name .. " must be slots")
  end
  self:commit(f, CMD.slot_move, { from, to })
end

function Record:slots_item_set(name, slot, field_name, value)
  local f = self:field(name)
  if f.wire_kind ~= "slots" then
    error("slots_item_set: " .. name .. " must be slots")
  end
  local item_field = find_item_field(f.item_meta, field_name)
  if not item_field then
    error("slots_item_set: unknown item field " .. tostring(field_name))
  end
  self:commit_item_change(f, slot, item_field, value)
end

function Record:slots_first_empty(name)
  local f = self:field(name)
  local slots = self.obj[f.name]
  for i = 0, (slots.size or 0) - 1 do
    if slots.by_slot[i] == nil then
      return i
    end
  end
  return nil
end

-- ---------- vec (complex item sequence) ----------

function Record:vec_push(name, item)
  local f = self:field(name)
  if f.wire_kind ~= "vec" then
    error("vec_push: " .. name .. " must be vec")
  end
  local pairs = self:encode_item_pairs(item, f.item_meta)
  self:commit(f, CMD.push, pairs)
end

function Record:vec_pop(name)
  local f = self:field(name)
  if f.wire_kind ~= "vec" then
    error("vec_pop: " .. name .. " must be vec")
  end
  self:commit(f, CMD.pop, nil)
end

function Record:vec_insert(name, idx, item)
  local f = self:field(name)
  if f.wire_kind ~= "vec" then
    error("vec_insert: " .. name .. " must be vec")
  end
  local pairs = self:encode_item_pairs(item, f.item_meta)
  self:commit(f, CMD.add, { idx, pairs })
end

function Record:vec_erase(name, idx, count)
  local f = self:field(name)
  if f.wire_kind ~= "vec" then
    error("vec_erase: " .. name .. " must be vec")
  end
  -- C++ erase_multi always encodes [idx, num]; bare erase encodes idx alone.
  if count ~= nil then
    self:commit(f, CMD.erase, { idx, count })
  else
    self:commit(f, CMD.erase, idx)
  end
end

function Record:vec_item_set(name, idx, field_name, value)
  local f = self:field(name)
  if f.wire_kind ~= "vec" then
    error("vec_item_set: " .. name .. " must be vec")
  end
  local item_field = find_item_field(f.item_meta, field_name)
  if not item_field then
    error("vec_item_set: unknown item field " .. tostring(field_name))
  end
  self:commit_item_change(f, idx, item_field, value)
end

Record.CMD = CMD
Record.resolve_mask = resolve_mask

return Record
