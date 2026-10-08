# channel_matrix — Client 同步 + DB PB 存档

在同一套 Player schema 上演示两条通道：

| 通道 | need_flags | 形态 |
|------|------------|------|
| Client | `sync_clients` | `codec_kind::json` + mutate 队列；Lua/C++ Replay |
| DB | `save_db` | `Player::to_pb` / `from_pb` 成员 → `.pb`；`codec both` 对拍；lua-protobuf 互通 |

## 依赖

```bash
luarocks install --local lua-protobuf   # require "pb" / "protoc"
# Go 1.21+ for tools/psync
```

## 目录

```text
dsl/                 # 唯一真相源（flags / items / player）
cpp/
  main_dual_channel.cpp
  replay_from_json.cpp
generated/
  cpp/               # PropFlags.h / *.h / *.cpp
  schema/ lua/ proto/
lua/                 # ChannelHub + lua-pb
```

与 `examples/rpg_player` **无编译依赖**。生成器：仓库根目录 `tools/psync`（Go 单二进制）。

## 怎么跑

```bash
cmake --build build --target channel_matrix_all -j
```
