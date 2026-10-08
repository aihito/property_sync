# channel_matrix 示例说明

完整代码：[`examples/channel_matrix/`](../examples/channel_matrix/)。

## 目标

- **C++ 属性 Record** → 客户端增量 + 观察者 Replay  
- **Lua 属性 Record / Replay**（源表 + meta 门面）  
- **Client 通道**：`sync_clients`（无 gold / item.name / ip）  
- **DB 通道**：`save_db` 全量视图 → **`PlayerSnapshot` PB**（[lua-protobuf](https://github.com/starwing/lua-protobuf)）存/载  

```bash
luarocks install --local lua-protobuf
# Go toolchain required for tools/psync
```

## 数据流

```text
Record (C++ / Lua ChannelHub)
   ├─ client mutates (need=sync_clients) → Client Replay → client_view
   └─ db_view = encode(save_db)
          → lua pb_archive.save → player_db.pb
          → lua pb_archive.load → load_snapshot
```

## 生成物位置

**真相源**：`examples/channel_matrix/dsl/*.psync`（与 `rpg_player` **零依赖**）。

全部在 `examples/channel_matrix/generated/`，由 **Go `psync`**（[`tools/psync`](../tools/psync/)）写出：

| 路径 | 内容 |
|------|------|
| `cpp/PropFlags.h` / `cpp/*.h` / `cpp/*.cpp` | 完整 C++ 类（组装 Meta mustache 片段） |
| `schema/` / `lua/` / `proto/` | 跨语言合同 / Record / Snapshot IDL |

Meta（`generate_property_sync`）仅为仓库遗留对照（如 `rpg_player`）；本示例不跑 libclang。

## 验收

```bash
cmake --build build --target channel_matrix_all -j
```

## 与 rpg_player

`rpg_player` 继续做 Meta 教学与过渡期双轨；本示例是 **DSL-only + Go psync** 双通道模板。
