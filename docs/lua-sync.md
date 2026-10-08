# 纯 Lua 同步约定

详见 [evolution-plan.md](./evolution-plan.md)。

## 产物

| 文件 | 说明 |
|------|------|
| `generated/lua/property_cmd.lua` | 与 C++ `property_cmd` 数值一致 |
| `generated/lua/property_runtime.lua` | **手写** Replay 引擎（可扩展；生成器原样拷贝） |
| `generated/lua/json.lua` | 轻量 JSON（对拍 / 宿主可选） |
| `generated/lua/<Class>_sync.lua` | 薄元数据：`fields` / `INDEX` / `apply_mutate` 委托 runtime |

## 架构（扩展点）

```text
*_sync.lua (生成)          property_runtime.lua (手写)
  SCHEMA / fields / flags ──► apply_mutate / encode_sync_view
  item_meta = Item_sync.META     bag / slots / vec / STL
```

- 新增容器语义：只改 `meta/lua_runtime/property_runtime.lua`，勿在 mustache 里堆逻辑。
- 新增属性类：重新跑 Meta；生成模块自动 `require` 子 item 的 `*_sync`。
- **无热更**：`SCHEMA_VERSION` 与客户端包锁定；版本不一致应直接拒绝。

## 字段元数据

每个 field：

| 键 | 含义 |
|----|------|
| `index` | 与 C++ property index 一致（0-based） |
| `name` | 去掉 `m_` 后的逻辑名 |
| `wire_kind` | `number/string/bool/array/vector/map/bag/slots/vec/object` |
| `flags` | 注解名列表（如 `sync_clients`、`save_db`） |
| `item_meta` | bag/slots/vec 时指向子类 `META`（含 `has_bag_id` / `has_slot`） |

`item_change` 载荷与 C++ 一致：`[item_or_slot_idx, record_offset, cmd, data]`。  
slots 的第一项是 **格子号**；bag/vec 是稠密下标。`record_offset` 按 `property_record_offset` 解码成字段 path。

## 对拍验收

```bash
# 一次跑完 C++ 导出 + batch / snapshot / mixed 三种对拍
cmake --build build --target rpg_player_lua_replay

# 或手动：
cd build/examples/rpg_player
./rpg_player_example
lua ../../../examples/rpg_player/lua_replay.lua ./generated/lua --batch \
  ./lua_mutates.json ./lua_sync_view.json
lua ../../../examples/rpg_player/lua_replay.lua ./generated/lua --snapshot \
  ./lua_final_snapshot.json ./lua_sync_view.json
lua ../../../examples/rpg_player/lua_replay.lua ./generated/lua --mixed \
  ./lua_checkpoint_snapshot.json ./lua_mutates_after_checkpoint.json ./lua_sync_view.json
```

导出文件：`lua_mutates.json`、`lua_sync_view.json`、`lua_checkpoint_snapshot.json`、`lua_mutates_after_checkpoint.json`、`lua_final_snapshot.json`。

## 使用示意

```lua
local PlayerSync = require("Player_sync")
local CMD = require("property_cmd")

local player = PlayerSync.new_default()
PlayerSync.apply_mutate(player, {
  offset = PlayerSync.INDEX.hp, -- replay offset；根字段单层时等于 index
  cmd = CMD.set,
  flag = 0,
  data = 80,
})
local view = PlayerSync.encode_sync_view(player) -- 对齐 C++ sync_clients
```

## 兼容

字段 index 变更规则见 [compatibility.md](./compatibility.md)。
