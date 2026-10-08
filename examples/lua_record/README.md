# lua_record — Lua Record / Replay 示例

DSL 真相源：`dsl/*.psync`。Go `psync` 生成到本目录 `generated/`。

| 目标 | 内容 |
|------|------|
| `lua_record_test` | Lua Record API + 与 C++ mutates 对拍 |
| `lua_record_replay` | CppRec → LuaRep（batch / snapshot / mixed） |
| `lua_record_cross` | 交叉矩阵 LuaRec↔CppRep |
| `lua_record_all` | 以上全部 |

```bash
cmake --build build --target lua_record_all -j
```

布局：

```text
dsl/                 # .psync
cpp/                 # main + replay_from_json
lua/                 # 测试脚本
generated/
  cpp/ schema/ lua/ proto/
```
