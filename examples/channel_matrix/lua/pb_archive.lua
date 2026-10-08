--[[
  DB archive via starwing/lua-protobuf (https://github.com/starwing/lua-protobuf).

    local Archive = require("pb_archive")
    Archive.init(proto_dir)
    local bin = Archive.save(db_view)
    local view = Archive.load(bin)
]]

local pb = require("pb")
local protoc = require("protoc")

local M = {
  TYPE = "property_sync.generated.PlayerSnapshot",
  _ready = false,
}

local function is_array(t)
  if type(t) ~= "table" then
    return false
  end
  local n = 0
  for _ in pairs(t) do
    n = n + 1
  end
  return n == 0 or t[1] ~= nil
end

--- Strip proto3 defaults so views compare with encode(ignore_default).
function M.strip_defaults(o)
  if type(o) ~= "table" then
    return o
  end
  if is_array(o) then
    local arr = {}
    for i, v in ipairs(o) do
      arr[i] = M.strip_defaults(v)
    end
    return arr
  end
  local out = {}
  for k, v in pairs(o) do
    -- nested Snapshot messages often decode schema_version=0; drop zeros / root keep 1
    if k == "schema_version" then
      if v and v ~= 0 then
        out[k] = v
      end
    elseif k == "slot" and v == 0 then
      -- omit proto3 default
    elseif v == nil or v == 0 or v == "" or v == false then
      -- keep only structural non-default-meaningful zeros
      if k == "sz" or k == "id" or k == "gold" then
        out[k] = v
      end
    elseif type(v) == "table" then
      local sv = M.strip_defaults(v)
      if is_array(sv) then
        if #sv > 0 then
          out[k] = sv
        end
      elseif next(sv) ~= nil then
        out[k] = sv
      end
    else
      out[k] = v
    end
  end
  return out
end

local function restore_slot_defaults(view)
  local eq = view.equipment
  if type(eq) == "table" and type(eq.data) == "table" then
    for _, it in ipairs(eq.data) do
      if type(it) == "table" and it.id ~= nil and it.slot == nil then
        it.slot = 0
      end
    end
  end
  return view
end

local function for_encode(view)
  local v = M.strip_defaults(view)
  if v.schema_version == nil then
    v.schema_version = 1
  end
  return v
end

function M.init(proto_dir)
  assert(proto_dir and #proto_dir > 0, "proto_dir required")
  local p = protoc.new()
  p.include_imports = true
  p:addpath(proto_dir)
  assert(p:loadfile("Player.proto"))
  M._ready = true
  M._proto_dir = proto_dir
  return M
end

function M.save(db_view)
  assert(M._ready, "call pb_archive.init(proto_dir) first")
  return assert(pb.encode(M.TYPE, for_encode(db_view)))
end

function M.load(bin)
  assert(M._ready, "call pb_archive.init(proto_dir) first")
  local msg = assert(pb.decode(M.TYPE, bin))
  msg = M.strip_defaults(msg)
  msg = restore_slot_defaults(msg)
  if msg.schema_version == nil then
    msg.schema_version = 1
  end
  return msg
end

function M.save_file(db_view, path)
  local bin = M.save(db_view)
  local f = assert(io.open(path, "wb"))
  f:write(bin)
  f:close()
  return #bin
end

function M.load_file(path)
  local f = assert(io.open(path, "rb"))
  local bin = f:read("*a")
  f:close()
  return M.load(bin), #bin
end

function M.roundtrip_ok(db_view)
  local back = M.load(M.save(db_view))
  local a = M.strip_defaults(db_view)
  local b = M.strip_defaults(back)
  a.schema_version, b.schema_version = nil, nil
  local function eq(x, y)
    if x == y then
      return true
    end
    if type(x) ~= type(y) then
      return false
    end
    if type(x) ~= "table" then
      return x == y
    end
    for k, v in pairs(x) do
      if not eq(v, y[k]) then
        return false
      end
    end
    for k, _ in pairs(y) do
      if x[k] == nil then
        return false
      end
    end
    return true
  end
  return eq(a, b), back
end

return M
