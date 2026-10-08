--[[
  channel_matrix: Lua Record dual-channel + lua-protobuf DB archive.

  Usage:
    lua channel_matrix.lua <generated/lua> <fixtures_dir> \
      [--proto-dir <generated/proto>] [--cpp-replay <bin>]
]]

local lua_dir = assert(arg[1], "need generated/lua dir")
local fix_dir = assert(arg[2], "need fixtures dir")
package.path = lua_dir .. "/?.lua;" .. package.path

-- channel_hub / pb_archive live next to this script
local script_dir = arg[0]:match("(.*/)") or "./"
package.path = script_dir .. "?.lua;" .. package.path

local json = require("json")
local PlayerMeta = require("Player_meta")
local Hub = require("channel_hub")

local cpp_bin, proto_dir
local i = 3
while i <= #arg do
  if arg[i] == "--cpp-replay" then
    cpp_bin = assert(arg[i + 1])
    i = i + 2
  elseif arg[i] == "--proto-dir" then
    proto_dir = assert(arg[i + 1])
    i = i + 2
  else
    i = i + 1
  end
end

--- Drop keys that proto3 JSON omits (defaults) so PB reload compares cleanly.
local function strip_defaults(o)
  if type(o) ~= "table" then
    return o
  end
  local out = {}
  local n = 0
  for k, v in pairs(o) do
    n = n + 1
  end
  local is_array = n > 0 and o[1] ~= nil
  if is_array then
    for i, v in ipairs(o) do
      out[i] = strip_defaults(v)
    end
    return out
  end
  for k, v in pairs(o) do
    if k ~= "schema_version" and not (k == "slot" and v == 0) then
      out[k] = strip_defaults(v)
    end
  end
  return out
end

local function deep_eq(a, b, path)
  path = path or "$"
  a, b = strip_defaults(a), strip_defaults(b)
  if a == b then
    return true
  end
  if type(a) ~= type(b) then
    if type(a) == "number" and type(b) == "number" and math.abs(a - b) < 1e-6 then
      return true
    end
    return false, path .. " type " .. type(a) .. "/" .. type(b)
  end
  if type(a) ~= "table" then
    if type(a) == "number" and math.abs(a - b) < 1e-6 then
      return true
    end
    return false, path .. " value"
  end
  for k, v in pairs(a) do
    local ok, err = deep_eq(v, b[k], path .. "." .. tostring(k))
    if not ok then
      return false, err
    end
  end
  for k, _ in pairs(b) do
    if a[k] == nil then
      return false, path .. "." .. tostring(k) .. " missing"
    end
  end
  return true
end

local function write_json(path, obj)
  local f = assert(io.open(path, "w"))
  f:write(json.encode(obj), "\n")
  f:close()
end

local function read_json(path)
  local f = assert(io.open(path, "r"))
  local s = f:read("*a")
  f:close()
  return json.decode(s)
end

local hub = Hub.open()
local rec = hub:facade()

rec.nickname = "Alice"
rec.level = 5
rec.hp = 100
rec.gold = 999
rec.pos = { 10.0, 0.0, 20.0 }
rec.pos:item_change(1, 3.5)
rec.tags = { "warrior", "pvp" }
rec.tags:push("guild")
rec.attrs.atk = 100

rec.inventory:insert({ id = 1001, count = 1, name = "HP Potion" })
rec.inventory[1001].count = 5
rec.inventory[1001].name = "Greater HP Potion"

rec.equipment:resize(4)
rec.equipment:insert({ id = 501, slot = 0, enhance = 0, name = "Iron Sword" })
rec.equipment[0].enhance = 3

rec.login_history:push({ login_ts = 100.0, logout_ts = 200.0, ip = "10.0.0.1" })
rec.login_history[0].ip = "10.0.0.8"

local client_batch, db_batch = hub:drain_split()
local client_view = hub:client_view({ ignore_default = true })
client_view.schema_version = 1
local db_view = hub:db_view({ ignore_default = true })
db_view.schema_version = 1 -- SCHEMA_VERSION; proto field number is 1000

-- gold only in db channel / db view
for _, m in ipairs(client_batch) do
  if m.offset == PlayerMeta.INDEX.gold then
    error("gold must not appear in client_batch")
  end
end
assert(db_view.gold == 999, "db_view.gold")
assert(client_view.gold == nil, "client_view no gold")

-- Lua client replay
local mirror = PlayerMeta.new_default()
assert(PlayerMeta.apply_batch(mirror, client_batch))
local rep_view = PlayerMeta.encode_sync_view(mirror, { ignore_default = true, need_flag_names = { "sync_clients" } })
local ok, err = deep_eq(client_view, rep_view)
if not ok then
  error("LuaRec→LuaClientRep: " .. tostring(err))
end
print(string.format("[PASS] LuaRec→LuaClientRep (%d client mutates)", #client_batch))

-- Lua DB: load_snapshot from db_view (JSON contract; PB via helper)
local db_mirror = PlayerMeta.new_default()
PlayerMeta.load_snapshot(db_mirror, db_view)
local db_rep = PlayerMeta.encode_sync_view(db_mirror, { ignore_default = true, need_flag_names = { "save_db" } })
ok, err = deep_eq(db_view, db_rep)
if not ok then
  error("Lua DB load_snapshot: " .. tostring(err))
end
print("[PASS] Lua DB load_snapshot(save_db view)")

write_json(fix_dir .. "/lua_client_mutates.json", client_batch)
write_json(fix_dir .. "/lua_client_view.json", client_view)
write_json(fix_dir .. "/lua_db_view.json", db_view)
print(string.format("[PASS] wrote lua fixtures (%d client / %d db mutates)", #client_batch, #db_batch))

if proto_dir then
  local Archive = require("pb_archive")
  local ok_pb, err_pb = pcall(function()
    Archive.init(proto_dir)
  end)
  if not ok_pb then
    error("lua-protobuf init failed (luarocks install lua-protobuf?): " .. tostring(err_pb))
  end
  local pb_path = fix_dir .. "/lua_player_db.pb"
  local n = Archive.save_file(db_view, pb_path)
  print(string.format("[PASS] lua-pb saved (%d bytes) → %s", n, pb_path))
  local from_pb = Archive.load_file(pb_path)
  write_json(fix_dir .. "/lua_db_view_from_pb.json", from_pb)
  local rt_ok = Archive.roundtrip_ok(db_view)
  if not rt_ok then
    error("lua-pb roundtrip mismatch")
  end
  print("[PASS] lua-pb roundtrip deep_equal")
  ok, err = deep_eq(db_view, from_pb)
  if not ok then
    error("Lua db_view vs PB reload: " .. tostring(err))
  end
  local pb_mirror = PlayerMeta.new_default()
  PlayerMeta.load_snapshot(pb_mirror, from_pb)
  local pb_rep = PlayerMeta.encode_sync_view(pb_mirror, {
    ignore_default = true,
    need_flag_names = { "save_db" },
  })
  ok, err = deep_eq(db_view, pb_rep)
  if not ok then
    error("Lua load_snapshot after lua-pb: " .. tostring(err))
  end
  print("[PASS] Lua DB lua-pb save/load + load_snapshot")
else
  print("[SKIP] PB archive (no --proto-dir)")
end

if cpp_bin then
  -- If C++ fixtures exist, compare client path optionally
  local cpp_view = fix_dir .. "/client_view.json"
  local f = io.open(cpp_view, "r")
  if f then
    f:close()
    -- C++ replay of lua client mutates
    local out = fix_dir .. "/cpp_rep_from_lua_client.json"
    local cmd = string.format("%s %s %s", cpp_bin, fix_dir .. "/lua_client_mutates.json", out)
    local rc = os.execute(cmd)
    if not (rc == true or rc == 0) then
      error("cpp replay failed")
    end
    local got = read_json(out)
    ok, err = deep_eq(client_view, got)
    if not ok then
      error("LuaRec→CppClientRep: " .. tostring(err))
    end
    print("[PASS] LuaRec→CppClientRep")
  else
    print("[SKIP] CppClientRep (no client_view.json yet)")
  end
else
  print("[SKIP] CppClientRep (no --cpp-replay)")
end

print("[PASS] channel_matrix lua done")
