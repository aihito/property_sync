# Docs

| 文档 | 内容 |
|------|------|
| [core-principles.md](./core-principles.md) | **入门必读**：核心原理（面向技术人员：定位、Record/Replay、Proxy、选型、Meta、边界） |
| [game-example.md](./game-example.md) | RPG 场景下的使用说明 |
| [channel-matrix.md](./channel-matrix.md) | Client 同步 + DB PB 存档双通道示例 |
| [build-and-test.md](./build-and-test.md) | 依赖安装、CMake 编译、代码生成与测试命令 |
| [evolution-plan.md](./evolution-plan.md) | 演进完整方案：兼容 / Protobuf / 纯 Lua（无热更） |
| [compatibility.md](./compatibility.md) | 字段兼容硬性规则与 CI 约定 |
| [protobuf.md](./protobuf.md) | Protobuf 字段号与双轨说明 |
| [lua-sync.md](./lua-sync.md) | 纯 Lua Replay 约定 |
| [lua-record.md](./lua-record.md) | Lua 写属性（Record）方案梳理：绑定 / 纯 Lua / 伪 Record |
| [dsl-multi-runtime.md](./dsl-multi-runtime.md) | **探索主线建议**：属性 DSL → IR → C++/Lua 均可 Record 或 Replay |
| [dsl-design.md](./dsl-design.md) | **`.psync` DSL 设计**：语法、类型、flag、校验、完整 rpg 示例 |
| [dsl-types.md](./dsl-types.md) | **数据类型总表**：DSL ↔ C++ 现状 ↔ Lua（含首版范围） |
| [dsl-implementation-plan.md](./dsl-implementation-plan.md) | **实施看板**：S0–S8 阶段、改动面、验收、PR 粒度 |
| [dsl-test.md](./dsl-test.md) | **DSL/IR/Emit 测试文档**：命令、判定、CMake 目标、排障 |
| [ir-schema.json](./ir-schema.json) | IR JSON Schema（`psync` 输出合同） |
| [`../dsl/`](../dsl/) | DSL 样例源文件（`flags` / `items` / `player`） |
| [`../tools/psync/`](../tools/psync/) | `.psync` → IR 编译器（S1） |
| [`../tools/psync/testdata/ir/`](../tools/psync/testdata/ir/) | IR golden 对拍 |

可运行示例代码：[`examples/rpg_player/`](../examples/rpg_player/)
