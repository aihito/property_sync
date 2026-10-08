--[[
  S5/S6/API: Pure-Lua Record — facade / FieldProxy / ItemProxy 全接口类型覆盖。

  Usage:
    lua lua_record_test.lua <lua_dir> [cpp_mutates.json]
]]

local lua_dir = assert(arg[1], "usage: lua lua_record_test.lua <lua_dir> [cpp_mutates.json]")
package.path = lua_dir .. "/?.lua;" .. package.path

local json = require("json")
local PlayerRecord = require("Player_record")
local PlayerMeta = require("Player_meta")

local function deep_equal(a, b)
  if a == b then
    return true
  end
  if a == nil and b == json.null then
    return true
  end
  if b == nil and a == json.null then
    return true
  end
  if type(a) ~= type(b) then
    return false
  end
  if type(a) ~= "table" then
    return a == b
  end
  for k, v in pairs(a) do
    if not deep_equal(v, b[k]) then
      return false
    end
  end
  for k, _ in pairs(b) do
    if a[k] == nil and b[k] ~= nil and b[k] ~= json.null then
      return false
    end
  end
  return true
end

local function assert_eq(got, want, msg)
  if not deep_equal(got, want) then
    error(string.format("%s\ngot=%s\nwant=%s", msg, json.encode(got), json.encode(want)))
  end
end

local function assert_fails(fn, needle, msg)
  local ok, err = pcall(fn)
  if ok then
    error(msg .. ": expected error")
  end
  if needle and not tostring(err):find(needle, 1, true) then
    error(string.format("%s: err=%s (want contains %q)", msg, tostring(err), needle))
  end
end

local function mutate_key(m)
  return { offset = m.offset, cmd = m.cmd, flag = m.flag, data = m.data }
end

local function roundtrip_ok(rec, label)
  local batch = rec:drain()
  local mirror = PlayerMeta.new_default()
  assert(PlayerMeta.apply_batch(mirror, batch), label .. " apply_batch")
  assert_eq(rec:view(), PlayerMeta.encode_sync_view(mirror), label .. " view")
  return batch
end

-- ============================================================================
-- API matrix: 按接口类型全面覆盖（正路径 + 拒绝路径）
-- ============================================================================
do
  local data = PlayerMeta.new_default()
  local rec = PlayerRecord.open(data)
  assert(rec:data() == data, "open: 源表 identity")

  -- ---- scalar (number / string) ----
  rec.nickname = "Alice"
  assert(rec.nickname == "Alice", "scalar read string")
  rec.hp = 80
  assert(rec.hp == 80, "scalar read number")
  rec:clear("hp")
  assert(rec.hp == 0, "clear hp → default 0")
  rec.hp = 100
  assert_fails(function()
    rec.hp = "bad"
  end, "expects number", "scalar type check")
  assert_fails(function()
    rec.nickname = 123
  end, "expects string", "string type check")
  assert_fails(function()
    rec.hp = nil
  end, "cannot delete", "scalar 防删")
  assert_fails(function()
    rec.not_a_field = 1
  end, "unknown field", "防野字段")

  -- ---- array ----
  rec.pos = { 1.0, 2.0, 3.0 }
  assert(type(rec.pos) == "table" and rec.pos.item_change, "array → FieldProxy")
  rec.pos:item_change(1, 9.5)
  assert(rec:data().pos[2] == 9.5, "array item_change")
  assert_fails(function()
    rec.pos = nil
  end, "cannot delete", "array 防删")
  assert_fails(function()
    rec.pos = 1
  end, "expects table", "array type check")

  -- ---- vector ----
  rec.tags = { "a", "b" }
  rec.tags:push("c")
  rec.tags:pop()
  rec.tags:push("c")
  rec.tags:push("d")
  rec.tags:erase(0)
  rec.tags:add(1, "x")
  assert(#rec:data().tags >= 2, "vector ops")

  -- ---- map / dict ----
  rec.attrs = { base = 1 }
  rec.attrs.atk = 100
  assert(rec.attrs.atk == 100, "dict subkey read")
  rec.attrs.def = 50
  rec.attrs.def = nil -- erase key
  assert(rec:data().attrs.def == nil, "dict key erase")
  rec.attrs:insert("hp_max", 500)
  rec.attrs:erase("base")
  assert_fails(function()
    rec.attrs = nil
  end, "cannot delete", "map 防删")

  -- ---- bag + ItemProxy ----
  assert_fails(function()
    rec.inventory = {}
  end, "cannot assign whole", "bag 禁止整表赋")
  assert_fails(function()
    rec.inventory = nil
  end, "cannot delete", "bag 防删")

  rec.inventory:insert({ id = 1001, count = 1, name = "HP Potion" })
  local it = rec.inventory[1001]
  assert(it ~= nil, "bag[id] ItemProxy")
  assert(it.count == 1, "ItemProxy read")
  it.count = 5
  it.name = "Greater HP Potion"
  assert(rec:data().inventory.items[1].count == 5, "ItemProxy write")
  assert(rec.inventory:get(1001).count == 5, "bag:get")
  assert(rec.inventory[9999] == nil, "bag missing id")

  local created = rec.inventory:get_insert(3001)
  assert(created ~= nil and created.id == 3001, "get_insert ItemProxy")
  created.count = 2

  assert_fails(function()
    rec.inventory[1001].count = nil
  end, "cannot delete item field", "ItemProxy 防删字段")
  assert_fails(function()
    rec.inventory[1001].nope = 1
  end, "unknown item field", "ItemProxy 野字段")
  assert_fails(function()
    rec.inventory[1001].id = 42
  end, "structural field", "ItemProxy 结构字段")
  assert_fails(function()
    rec.inventory[1001].count = "x"
  end, "expects number", "ItemProxy 类型")

  rec.inventory:insert({ id = 2001, count = 10, name = "Ore" })
  rec.inventory:erase(2001)

  -- ---- bag buffs ----
  rec.buffs:insert({ id = 200, level = 1, expire_ts = 1000.0 })
  rec.buffs[200].level = 3
  rec.buffs[200].expire_ts = 9999.0

  -- ---- slots + ItemProxy ----
  assert_fails(function()
    rec.equipment = {}
  end, "cannot assign whole", "slots 禁止整表赋")
  assert(rec.equipment:insert({ id = 1, slot = 0, name = "Wood" }) == false, "slots before resize")
  local q0 = #rec:peek()
  rec.equipment:insert({ id = 1, slot = 0, name = "Wood" })
  assert(#rec:peek() == q0, "no enqueue before resize")

  rec.equipment:resize(6)
  rec.equipment:insert({ id = 501, slot = 0, enhance = 0, name = "Iron Sword" })
  local empty = rec.equipment:first_empty()
  assert(empty == 1, "first_empty")
  rec.equipment:insert({ id = 502, slot = empty, enhance = 1, name = "Wood Shield" })
  assert(rec.equipment[0] ~= nil, "slots[slot] ItemProxy")
  rec.equipment[0].enhance = 3
  assert(rec:data().equipment.by_slot[0].enhance == 3, "slots ItemProxy write")
  rec.equipment:swap(0, 1)
  rec.equipment:move(1, 3)
  rec.equipment:erase_id(502)
  assert(rec:data().equipment.by_id[502] == nil, "erase_id removes by_id")
  assert(rec.equipment[3] ~= nil and rec.equipment[3].id == 501, "501 remains at slot 3")

  -- ---- vec + ItemProxy ----
  assert_fails(function()
    rec.login_history = {}
  end, "cannot assign whole", "vec 禁止整表赋")
  rec.login_history:push({ login_ts = 100.0, logout_ts = 200.0, ip = "10.0.0.1" })
  rec.login_history:push({ login_ts = 300.0, logout_ts = 0.0, ip = "10.0.0.1" })
  assert(rec.login_history[1] ~= nil, "vec[idx] ItemProxy")
  rec.login_history[1].logout_ts = 400.0
  rec.login_history[1].ip = "10.0.0.2"
  rec.login_history:insert(1, { login_ts = 250.0, logout_ts = 280.0, ip = "10.0.0.8" })
  rec.login_history:erase(0, 1)
  rec.login_history:push({ login_ts = 300.0, logout_ts = 0.0, ip = "10.0.0.1" })
  rec.login_history:pop()

  -- ---- flag: gold 不入 sync_clients 队列 ----
  rec.gold = 999
  local batch_api = roundtrip_ok(rec, "API matrix")
  for _, m in ipairs(batch_api) do
    if m.offset == PlayerMeta.INDEX.gold then
      error("gold should not enqueue under sync_clients")
    end
  end
  print(string.format("[PASS] API matrix (scalar/array/vector/map/bag/slots/vec + rejects, %d mutates)", #batch_api))
end

-- ============================================================================
-- S5: 场景回放（标量/array/vector/map）
-- ============================================================================
do
  local rec = PlayerRecord.new()
  rec.nickname = "Alice"
  rec.hp = 80
  rec:clear("hp")
  rec.hp = 100
  rec.level = 5
  rec.pos = { 10.0, 0.0, 20.0 }
  rec.pos:item_change(1, 3.5)
  rec.tags = { "newbie", "warrior" }
  rec.tags:push("vip")
  rec.tags:pop()
  rec.tags:push("pvp")
  rec.tags:push("guild")
  rec.tags:erase(0)
  rec.attrs.atk = 100
  rec.attrs.def = 50
  rec.attrs.def = nil
  rec.attrs.hp_max = 500
  rec.gold = 999

  local batch_s5 = roundtrip_ok(rec, "S5")
  print(string.format("[PASS] S5 Lua Record self roundtrip (%d mutates)", #batch_s5))
end

-- ============================================================================
-- S6: bag / slots / vec（ItemProxy）+ 可选 C++ mutate deep_equal
-- ============================================================================
local batch_s6
do
  local rec2 = PlayerRecord.new()
  rec2.inventory:insert({ id = 1001, count = 1, name = "HP Potion" })
  rec2.inventory[1001].count = 5
  rec2.inventory[1001].name = "Greater HP Potion"
  rec2.inventory:insert({ id = 2001, count = 10, name = "Iron Ore" })
  local it3001 = rec2.inventory:get_insert(3001)
  it3001.count = 2
  rec2.inventory:erase(2001)

  rec2.buffs:insert({ id = 200, level = 1, expire_ts = 1000.0 })
  rec2.buffs[200].level = 3
  rec2.buffs[200].expire_ts = 9999.0

  assert(rec2.equipment:insert({ id = 1, slot = 0, enhance = 0, name = "Wood Sword" }) == false)
  local q_before = #rec2:peek()
  rec2.equipment:insert({ id = 1, slot = 0, enhance = 0, name = "Wood Sword" })
  assert(#rec2:peek() == q_before, "no enqueue before resize")

  rec2.equipment:resize(6)
  rec2.equipment:insert({ id = 501, slot = 0, enhance = 0, name = "Iron Sword" })
  local empty = rec2.equipment:first_empty()
  rec2.equipment:insert({ id = 502, slot = empty, enhance = 1, name = "Wood Shield" })
  rec2.equipment[0].enhance = 3
  rec2.equipment:swap(0, 1)
  rec2.equipment:move(1, 3)
  rec2.equipment:erase_id(502)

  rec2.login_history:push({ login_ts = 100.0, logout_ts = 200.0, ip = "10.0.0.1" })
  rec2.login_history:push({ login_ts = 300.0, logout_ts = 0.0, ip = "10.0.0.1" })
  rec2.login_history[1].logout_ts = 400.0
  rec2.login_history[1].ip = "10.0.0.2"
  rec2.login_history:insert(1, { login_ts = 250.0, logout_ts = 280.0, ip = "10.0.0.8" })
  rec2.login_history:erase(0, 1)
  rec2.login_history:push({ login_ts = 300.0, logout_ts = 0.0, ip = "10.0.0.1" })
  rec2.login_history:pop()

  batch_s6 = roundtrip_ok(rec2, "S6")
  print(string.format("[PASS] S6 Lua Record self roundtrip (%d mutates)", #batch_s6))
end

local cpp_path = arg[2]
if cpp_path then
  local fh = assert(io.open(cpp_path, "r"))
  local cpp_all = json.decode(fh:read("*a"))
  fh:close()
  local cpp, got = {}, {}
  for _, m in ipairs(cpp_all) do
    if m.offset == 7 or m.offset == 8 or m.offset == 9 or m.offset == 10 then
      cpp[#cpp + 1] = mutate_key(m)
    end
  end
  for _, m in ipairs(batch_s6) do
    got[#got + 1] = mutate_key(m)
  end
  if #got ~= #cpp then
    error(string.format("S6 mutate count got=%d want=%d", #got, #cpp))
  end
  for i = 1, #got do
    if not deep_equal(got[i], cpp[i]) then
      error(string.format("S6 mutate[%d] mismatch\ngot=%s\nwant=%s", i, json.encode(got[i]), json.encode(cpp[i])))
    end
  end
  print(string.format("[PASS] S6 Lua Record mutates deep_equal C++ (%d msgs, offsets 7-10)", #got))
else
  print("[SKIP] S6 vs C++ (no cpp_mutates.json)")
end

print("[PASS] lua_record_test all checks")
