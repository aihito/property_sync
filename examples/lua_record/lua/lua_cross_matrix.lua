--[[
  S7 cross matrix harness.

  Usage:
    lua lua_cross_matrix.lua <lua_dir> <work_dir> [--cpp-replay <bin>]
]]

local lua_dir = assert(arg[1], "usage: lua_cross_matrix.lua <lua_dir> <work_dir> [--cpp-replay bin]")
local work_dir = assert(arg[2], "need work_dir")
package.path = lua_dir .. "/?.lua;" .. package.path

local json = require("json")
local Record = require("property_record")
local PlayerMeta = require("Player_meta")

local cpp_bin = nil
local i = 3
while i <= #arg do
  if arg[i] == "--cpp-replay" then
    cpp_bin = assert(arg[i + 1])
    i = i + 2
  else
    i = i + 1
  end
end

local function deep_eq(a, b, path)
  path = path or "$"
  if a == b then
    return true
  end
  if type(a) ~= type(b) then
    return false, path .. " type"
  end
  if type(a) ~= "table" then
    if type(a) == "number" and type(b) == "number" and math.abs(a - b) < 1e-6 then
      return true
    end
    return false, path .. " value"
  end
  for k, v in pairs(a) do
    if k ~= "schema_version" then
      local ok, err = deep_eq(v, b[k], path .. "." .. tostring(k))
      if not ok then
        return false, err
      end
    end
  end
  for k, _ in pairs(b) do
    if k ~= "schema_version" and a[k] == nil then
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

local function run_lua_record_scenario()
  -- 显式 源表 + meta 门面
  local data = PlayerMeta.new_default()
  local rec = Record.bind(PlayerMeta, { obj = data })

  rec.nickname = "Alice"
  rec.level = 5
  rec.hp = 100
  rec.pos = { 10.0, 0.0, 20.0 }
  rec.pos:item_change(1, 3.5)
  rec.tags = { "warrior", "pvp", "guild" }
  rec.attrs.atk = 100
  rec.attrs.hp_max = 500

  rec.inventory:insert({ id = 1001, count = 1, name = "HP Potion" })
  rec.inventory[1001].count = 5
  rec.inventory:insert({ id = 2001, count = 10, name = "Iron Ore" })
  rec.inventory:get_insert(3001).count = 2
  rec.inventory:erase(2001)

  rec.buffs:insert({ id = 200, level = 1, expire_ts = 1000.0 })
  rec.buffs[200].level = 3
  rec.buffs[200].expire_ts = 9999.0

  assert(rec.equipment:insert({ id = 1, slot = 0, name = "Wood" }) == false)
  rec.equipment:resize(6)
  rec.equipment:insert({ id = 501, slot = 0, enhance = 0, name = "Iron Sword" })
  local empty = rec.equipment:first_empty()
  rec.equipment:insert({ id = 502, slot = empty, enhance = 1, name = "Wood Shield" })
  rec.equipment[0].enhance = 3
  rec.equipment:swap(0, 1)
  rec.equipment:move(1, 3)
  rec.equipment:erase_id(502)

  rec.login_history:push({ login_ts = 100.0, logout_ts = 200.0, ip = "10.0.0.1" })
  rec.login_history:push({ login_ts = 300.0, logout_ts = 0.0, ip = "10.0.0.1" })
  rec.login_history[1].logout_ts = 400.0
  rec.login_history:insert(1, { login_ts = 250.0, logout_ts = 280.0, ip = "10.0.0.8" })
  rec.login_history:erase(0, 1)
  rec.login_history:push({ login_ts = 300.0, logout_ts = 0.0, ip = "10.0.0.1" })
  rec.login_history:pop()

  local batch = rec:drain()
  local view = rec:view({ ignore_default = true })
  view.schema_version = 1
  assert(rec:data() == data, "源表 identity")
  return batch, view
end

local mutates_path = work_dir .. "/cross_lua_mutates.json"
local lua_view_path = work_dir .. "/cross_lua_view.json"
local cpp_view_path = work_dir .. "/cross_cpp_view.json"

local batch, lua_view = run_lua_record_scenario()
local mirror = PlayerMeta.new_default()
assert(PlayerMeta.apply_batch(mirror, batch))
local lua_rep_view = PlayerMeta.encode_sync_view(mirror, { ignore_default = true })
lua_rep_view.schema_version = 1
local ok, err = deep_eq(lua_view, lua_rep_view)
if not ok then
  io.stderr:write("[FAIL] LuaRec→LuaRep: " .. tostring(err) .. "\n")
  os.exit(1)
end
print(string.format("[PASS] LuaRec→LuaRep (%d mutates)", #batch))

write_json(mutates_path, batch)
write_json(lua_view_path, lua_view)

local results = {
  { "CppRec", "CppRep", "ok (lua_record_cpp)" },
  { "CppRec", "LuaRep", "ok (lua_record_replay)" },
  { "LuaRec", "LuaRep", "PASS" },
  { "LuaRec", "CppRep", "PENDING" },
}

if cpp_bin then
  local cmd = string.format("%s %s %s", cpp_bin, mutates_path, cpp_view_path)
  local rc = os.execute(cmd)
  if not (rc == true or rc == 0) then
    io.stderr:write("[FAIL] C++ replay binary failed\n")
    os.exit(1)
  end
  local cpp_view = read_json(cpp_view_path)
  local eq, detail = deep_eq(lua_view, cpp_view)
  if not eq then
    io.stderr:write("[FAIL] LuaRec→CppRep: " .. tostring(detail) .. "\n")
    os.exit(1)
  end
  print(string.format("[PASS] LuaRec→CppRep (%d mutates)", #batch))
  results[4][3] = "PASS"
else
  print("[SKIP] LuaRec→CppRep")
  results[4][3] = "SKIP"
end

print("")
print("=== S7 Cross Matrix ===")
print(string.format("%-8s %-8s %s", "Record", "Replay", "Status"))
print(string.rep("-", 40))
for _, row in ipairs(results) do
  print(string.format("%-8s %-8s %s", row[1], row[2], row[3]))
end
print("[PASS] lua_cross_matrix done")
