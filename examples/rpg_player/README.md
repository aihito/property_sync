# RPG Player 示例

用「玩家昵称 / 血量 / 金币 / 道具背包 / Buff 背包」演示属性增量同步。

原理说明见 [`docs/game-example.md`](../../docs/game-example.md)。

## 文件

| 文件 | 说明 |
|------|------|
| `prop_flags.h` | 同步 / 存库 flag |
| `macro.h` | `Meta(...)` 标注宏 |
| `rpg_items.h` | Item / Buff 定义 |
| `rpg_player.h` | Player 根属性 |
| `main.cpp` | Server record → Client replay 演示 |

# 构建（在仓库根目录，需已安装 any_container / nlohmann_json，并能找到 Clang）

```bash
cmake -S . -B build -DCMAKE_PREFIX_PATH=/path/to/deps
cmake --build build --target rpg_player_example -j
./build/examples/rpg_player/rpg_player_example
```

CMake 会先编译 `generate_property_sync`，再自动生成 `Item` / `Buff` / `Player` 的 inch 文件，最后编译本示例。可用 `-DWITH_EXAMPLES=OFF` 关闭。

## 演示流程（main 输出）

1. 改昵称、扣血 → 观察者收到 set  
2. 加药水并改 `count` → insert + item_change  
3. 加 Buff 并叠层 → insert + item_change  
4. 改金币 → **不进** sync_clients 队列（仅 save_db）  
5. 全部 replay 后打印 server / client encode，可见字段一致  
