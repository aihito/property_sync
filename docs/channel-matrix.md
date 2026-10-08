# channel_matrix 示例说明

完整代码：[`examples/channel_matrix/`](../examples/channel_matrix/)。

## 目标

- **C++ 属性 Record** → 客户端增量 + 观察者 Replay  
- **Lua 属性 Record / Replay**（源表 + meta 门面）  
- **Client 通道**：`sync_clients` + **`codec_kind::json`**（无 gold / item.name / ip）  
- **DB 通道**：`save_db` + **`codec_kind::protobuf`**（`PlayerSnapshot` PB）；`both` 做 C++ roundtrip  
- **互通**：C++ 写出的 `player_db.pb` 可被 [lua-protobuf](https://github.com/starwing/lua-protobuf) `load`

```bash
luarocks install --local lua-protobuf
# Go toolchain for tools/psync
# Fedora: sudo dnf install protobuf-devel   # C++ Snapshot codec
```

## 数据流

```text
Record (C++ / Lua ChannelHub)
   ├─ client mutates (need=sync_clients) → Client Replay
   │     → encode_snapshot(..., codec_kind::json) → client_view.json
   └─ encode_snapshot(..., codec_kind::protobuf) → player_db.pb
          → lua pb_archive.load → load_snapshot
          → both roundtrip：JSON view ≡ PB decode 再 encode
```

API：[`include/property_codec.h`](../include/property_codec.h)；PB 为类成员 `to_pb`/`from_pb`（见生成的 `*.h`/`*.cpp`）。详见 [protobuf.md](./protobuf.md)。

## 生成物位置

**真相源**：`examples/channel_matrix/dsl/*.psync`（与 `rpg_player` **零依赖**）。

全部在 `examples/channel_matrix/generated/`，由 **Go `psync`**（[`tools/psync`](../tools/psync/)）写出：

| 路径 | 内容 |
|------|------|
| `cpp/PropFlags.h` / `cpp/*.h` / `cpp/*.cpp` | 完整 C++ 类 |
| `schema/` / `lua/` / `proto/` | 跨语言合同 / Record / Snapshot IDL |

构建目录另有 `pb_gen/*.pb.cc`（`protoc --cpp_out`，需 `WITH_PROTOBUF`）。

Meta（`generate_property_sync`）仅为仓库遗留对照（如 `rpg_player`）；本示例不跑 libclang。

## 验收

```bash
cmake -S . -B build -DWITH_PROTOBUF=ON
cmake --build build --target channel_matrix_all -j
```

## 与 rpg_player

`rpg_player` 继续做 Meta 教学与过渡期双轨；本示例是 **DSL-only + Go psync + Codec** 双通道模板。
