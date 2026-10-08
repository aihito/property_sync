# RPG Player 示例（C++ / Meta）

覆盖 **基础值 / array / vector / map / bag / slots / vec / flag**。  
**不含 Lua 测试**（见 [`../lua_record`](../lua_record/)）。

生成物目录：本示例下 `generated/`（Meta → `*.inch` / `*.incpp` / lua / proto / schema）。

## 文件

| 文件 | 说明 |
|------|------|
| `rpg_items.h` / `rpg_player.h` | Meta 注解属性类 |
| `main.cpp` | Record → Replay 演示 |
| `replay_from_json.cpp` | C++ Replay 工具 |
| `prop_flags.h` / `macro.h` | flag 与 Meta 宏 |
| `generated/` | 全部生成文件 |

## 构建

```bash
cmake --build build --target rpg_player_example -j
./build/examples/rpg_player/rpg_player_example
```
