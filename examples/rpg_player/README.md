# RPG Player 示例

覆盖 **基础值 / array / vector / map / bag / slots / vec / flag**，运行时按章节打印每条同步消息。

说明文档：[`docs/game-example.md`](../../docs/game-example.md)。

## 文件

| 文件 | 说明 |
|------|------|
| `rpg_items.h` | Item(bag) / Buff(bag) / EquipItem(slots) / LoginRecord(vec) |
| `rpg_player.h` | Player 根属性 |
| `main.cpp` | 分 10 节的 Record → Replay 演示 |
| `prop_flags.h` / `macro.h` | flag 与 Meta 宏 |

## 构建

```bash
cmake -S . -B build -DCMAKE_PREFIX_PATH=/path/to/deps
cmake --build build --target rpg_player_example -j
./build/examples/rpg_player/rpg_player_example
```

CMake 会自动跑 `generate_property_sync` 生成 inch。关闭示例：`-DWITH_EXAMPLES=OFF`。

## 章节对照

1. 基础值 set/clear  
2. array 坐标  
3. vector 标签  
4. map 属性  
5. bag 道具（含仅存库的 name）  
6. bag Buff 叠层  
7. slots 装备栏（resize/swap/move）  
8. vec 登录记录（顺序/中间插入）  
9. flag：金币不同步给观察者  
10. 最终 PASS  
