#!/usr/bin/env lua
-- Pure-Lua 对拍 helpers for property_sync.
-- Usage:
--   lua lua_replay.lua <lua_dir> --batch <mutates.json> <expect.json>
--   lua lua_replay.lua <lua_dir> --snapshot <snapshot.json> <expect.json>
--   lua lua_replay.lua <lua_dir> --mixed <snapshot.json> <delta_mutates.json> <expect.json>
-- Backward compatible:
--   lua lua_replay.lua <lua_dir> <mutates.json> <expect.json>   (== --batch)

local lua_dir = assert(arg[1], "usage: lua_replay.lua <lua_dir> (--batch|--snapshot|--mixed) ...")

package.path = lua_dir .. "/?.lua;" .. package.path

local JSON = require("json")
local PlayerSync = require("Player_sync")

local function read_file(path)
  local f = assert(io.open(path, "rb"))
  local s = f:read("*a")
  f:close()
  return s
end

local function deep_eq(a, b, path)
  path = path or "$"
  local ta, tb = type(a), type(b)
  if ta ~= tb then
    return false, string.format("%s type %s ~= %s", path, ta, tb)
  end
  if ta ~= "table" then
    if a ~= b then
      if ta == "number" and math.abs(a - b) < 1e-9 then
        return true
      end
      return false, string.format("%s value %s ~= %s", path, tostring(a), tostring(b))
    end
    return true
  end
  local keys = {}
  local seen = {}
  for k in pairs(a) do
    keys[#keys + 1] = k
    seen[k] = true
  end
  for k in pairs(b) do
    if not seen[k] then
      keys[#keys + 1] = k
    end
  end
  table.sort(keys, function(x, y)
    return tostring(x) < tostring(y)
  end)
  for _, k in ipairs(keys) do
    local ok, err = deep_eq(a[k], b[k], path .. "." .. tostring(k))
    if not ok then
      return false, err
    end
  end
  return true
end

--- Drop schema_version from expect views (encode_sync_view does not emit it).
local function strip_schema(t)
  if type(t) ~= "table" then
    return t
  end
  local out = {}
  for k, v in pairs(t) do
    if k ~= "schema_version" then
      out[k] = v
    end
  end
  return out
end

local function fail_apply(ok, err, bad, idx)
  if ok then
    return
  end
  io.stderr:write(string.format("[FAIL] apply_mutate #%s: %s\n  msg=%s\n",
    tostring(idx), tostring(err), JSON.encode(bad)))
  os.exit(1)
end

local function check_view(got, expect, label)
  local eq, detail = deep_eq(got, strip_schema(expect))
  if not eq then
    io.stderr:write("[FAIL] " .. label .. " mismatch: " .. tostring(detail) .. "\n")
    io.stderr:write("got=" .. JSON.encode(got) .. "\n")
    io.stderr:write("exp=" .. JSON.encode(strip_schema(expect)) .. "\n")
    os.exit(1)
  end
end

local mode = arg[2]
local a3, a4, a5 = arg[3], arg[4], arg[5]

-- legacy: <lua_dir> <mutates> <expect>
if mode and mode:sub(1, 2) ~= "--" then
  a5 = nil
  a4 = a3
  a3 = mode
  mode = "--batch"
end

if mode == "--batch" then
  local mutates = JSON.decode(read_file(assert(a3)))
  local expect = JSON.decode(read_file(assert(a4)))
  local player = PlayerSync.new_default()
  fail_apply(PlayerSync.apply_batch(player, mutates))
  check_view(PlayerSync.encode_sync_view(player, { ignore_default = true }), expect, "batch")
  print(string.format("[PASS] Lua batch replay %d mutates; sync_clients view matches C++", #mutates))
elseif mode == "--snapshot" then
  local snap = JSON.decode(read_file(assert(a3)))
  local expect = JSON.decode(read_file(assert(a4)))
  local player = PlayerSync.new_default()
  PlayerSync.load_snapshot(player, snap)
  check_view(PlayerSync.encode_sync_view(player, { ignore_default = true }), expect, "snapshot")
  print("[PASS] Lua load_snapshot; sync_clients view matches C++")
elseif mode == "--mixed" then
  local snap = JSON.decode(read_file(assert(a3)))
  local delta = JSON.decode(read_file(assert(a4)))
  local expect = JSON.decode(read_file(assert(a5)))
  local player = PlayerSync.new_default()
  PlayerSync.load_snapshot(player, snap)
  fail_apply(PlayerSync.apply_batch(player, delta))
  check_view(PlayerSync.encode_sync_view(player, { ignore_default = true }), expect, "mixed")
  print(string.format(
    "[PASS] Lua mixed: snapshot + %d delta mutates; sync_clients view matches C++",
    #delta))
else
  io.stderr:write("unknown mode: " .. tostring(mode) .. "\n")
  io.stderr:write("use --batch | --snapshot | --mixed\n")
  os.exit(2)
end

os.exit(0)
