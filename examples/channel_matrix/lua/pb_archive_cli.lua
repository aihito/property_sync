--[[
  CLI for lua-protobuf PlayerSnapshot archive.

  Usage:
    lua pb_archive_cli.lua <proto_dir> <lua_module_dir> save <db_view.json> <out.pb>
    lua pb_archive_cli.lua <proto_dir> <lua_module_dir> load <in.pb> <out_view.json>
    lua pb_archive_cli.lua <proto_dir> <lua_module_dir> roundtrip <db_view.json>
]]

local proto_dir = assert(arg[1], "proto_dir")
local lua_dir = assert(arg[2], "lua_module_dir (for json.lua + pb_archive)")
local cmd = assert(arg[3], "save|load|roundtrip")

package.path = lua_dir .. "/?.lua;" .. package.path
-- also this script's directory for pb_archive.lua
local script_dir = (arg[0] or ""):match("(.*/)") or "./"
package.path = script_dir .. "?.lua;" .. package.path

local json = require("json")
local Archive = require("pb_archive")
Archive.init(proto_dir)

local function read_json(path)
  local f = assert(io.open(path, "r"))
  local s = f:read("*a")
  f:close()
  return json.decode(s)
end

local function write_json(path, obj)
  local f = assert(io.open(path, "w"))
  f:write(json.encode(obj), "\n")
  f:close()
end

if cmd == "save" then
  local view = read_json(assert(arg[4]))
  local n = Archive.save_file(view, assert(arg[5]))
  print(string.format("[PASS] lua-pb saved PlayerSnapshot (%d bytes) → %s", n, arg[5]))
elseif cmd == "load" then
  local view, n = Archive.load_file(assert(arg[4]))
  write_json(assert(arg[5]), view)
  print(string.format("[PASS] lua-pb loaded PlayerSnapshot (%d bytes) → %s", n, arg[5]))
elseif cmd == "roundtrip" then
  local view = read_json(assert(arg[4]))
  local ok = Archive.roundtrip_ok(view)
  if not ok then
    io.stderr:write("[FAIL] lua-pb roundtrip mismatch\n")
    os.exit(1)
  end
  print("[PASS] lua-pb roundtrip deep_equal")
else
  io.stderr:write("unknown cmd: " .. tostring(cmd) .. "\n")
  os.exit(2)
end
