# DSL 方案实施计划

> **前提：** DSL 设计已认可（[dsl-design.md](./dsl-design.md)、[dsl-types.md](./dsl-types.md)、`tools/psync/testdata/dsl/*.psync`）。  
> **目标：** 把真相源从「C++ Meta 注解」迁到「`.psync` → IR → 多语言 emitter」，并具备 C++/Lua Record·Replay 对称能力。  
> **原则：** 小步可回滚；每阶段现有对拍不回退；先合同与管道，再 Lua Record。

---

## 1. 现状 → 目标

| | 现在 | 目标 |
|--|------|------|
| 真相源 | `Meta(property)` + libclang | `.psync` DSL |
| 中间层 | `ClassModel`（生成器内隐式） | 显式 **IR JSON**（可落盘、可 diff） |
| C++ Rec/Rep | Meta 生成 inch | IR → 同产物（可暂双轨） |
| Lua Rep | Meta 生成 + runtime | IR → 生成（runtime 适配 `list`/`dict`） |
| Lua Rec | 无 | IR → `*_meta.lua` + `property_record.bind` |
| 验收 | C++ 测 + Lua batch/snapshot/mixed | + DSL→IR golden；+ 交叉 Rec/Rep |

```text
[已完成设计]  tools/psync/testdata/dsl/*.psync + 文档
      │
      ▼
  Parser → IR → Emitters（分阶段接）
      │
      ▼
  对拍矩阵全绿 → 可选：rpg 头文件改为生成物 / 弱化 libclang
```

---

## 2. 阶段总表

| 阶段 | 主题 | 主要改动面 | 验收 | 预估 |
|------|------|------------|------|------|
| **S0** ✅ | 冻结合同 | 文档状态；IR JSON Schema；样例冻结 | 评审打勾 | 0.5d |
| **S1** ✅ | DSL → IR | 新工具 `tools/psync`（建议 Python） | `testdata/dsl/*.psync` → IR 与 golden 一致 | 2–3d |
| **S2** ✅ | IR → 现有产物（旁路） | `psync emit`（IR→schema/lua/proto） | 与 Meta 产物语义对拍 | 2–3d |
| **S3** ✅ | 切 rpg 生成源 | CMake 双轨 + dsl_check；inch 仍 Meta | `rpg_player_*` + dsl_check | 1–2d |
| **S4** ✅ | Lua wire 对齐 | runtime 认 `list`/`dict`（兼容旧 `vector`/`map`） | 现有 lua_replay 仍 PASS | 0.5–1d |
| **S5** ✅ | Lua Record（标量+简单容器） | `property_record.lua` + 生成 API | Lua Rec→Lua Rep；队列与 C++ 子集对拍 | 3–5d |
| **S6** ✅ | Lua Record（bag/slots/vec） | 补齐入队语义 | 与 C++ Record 队列 deep_equal（rpg 场景） | 3–5d |
| **S7** ✅ | 交叉矩阵 | 测试 harness | LuaRec→CppRep、CppRec→LuaRep | 2d |
| **S8** ✅ | 收尾 | 文档、Meta 路径保留作 inch | 探索结论 | 1d |
| **S9** ✅ | DSL → C++ | `emit_ctx` + `emit_cpp` 复用 Meta mustache；`channel_matrix` DSL-only | inch≡Meta；`channel_matrix_all` 绿 | — |
| **S10** ✅ | Go psync | `tools/psync` Go 单二进制替换 Python；工具链默认 Go | `go test` + `channel_matrix_all` | — |

并行可选：S2 期间整理 **wire×cmd 语义表**（从 C++ 测例抽取），供 S5/S6 对照。

---

## 3. 各阶段细则

### S0 — 冻结合同（当前即可勾选）

**已有：**

- [x] `docs/dsl-design.md` / `dsl-types.md` / `dsl-multi-runtime.md`  
- [x] `tools/psync/testdata/dsl/{flags,items,player}.psync`  

**已完成：**

- [x] `docs/ir-schema.json`：IR 字段必填项、`wire_kind` / `kind` 枚举  
- [x] 本文作为实施看板；`dsl-design.md` §14 已指向本文  
- [x] `tools/psync/testdata/ir/*.ir.json` golden（由 `psync compile` 生成并对拍）  

**验收：** IR schema 能描述 Player/Item 全字段；与 `dsl-types` 首版类型集一致。

---

### S1 — Parser：`.psync` → IR ✅

**交付物：**

```text
tools/psync/
  lexer.py / parser.py   # 递归下降
  models.py              # IR dataclass
  validate.py            # V1–V7（可插拔规则列表）
  compile.py             # import 图 → CompilationUnit
  cli.py / __main__.py   # compile | check | dump
```

**用法：**

```bash
cd <repo>
go -C tools/psync build -o /tmp/psync ./cmd/psync
/tmp/psync check tools/psync/testdata/dsl/player.psync --root tools/psync/testdata
/tmp/psync compile tools/psync/testdata/dsl/player.psync -o /tmp/ir --root tools/psync/testdata
go -C tools/psync test ./...
```

**输出示例：** `Player.ir.json`、`Item.ir.json`、…、`RpgFlags.ir.json`（golden 在 `tools/psync/testdata/ir/`）

**IR 字段：**  
`name, kind, namespace, schema_version, fields[{index,name,type,wire_kind,flags,default,deprecated,item_class}], reserved[], key_type?, flags_ref, source_file`

**不做：** 还不改 C++ 生成器（→ S2）。

---

### S2 — IR 驱动现有 emitter（双轨）✅

**策略：** 不立刻删 libclang；Python 旁路先对齐 schema/lua/proto（C++ `ClassModel::from_ir` / inch 延后到 S3）。

```text
路径 A（现行）：头文件 Meta → generate_property_sync → 产物
路径 B（新）：  DSL → IR → psync emit → schema/ / lua/ / proto/
```

**已交付：**

| 项 | 做法 |
|----|------|
| `tools/psync/emit.py` | IR→schema/lua/proto；`list`→`vector`、`dict`→`map`（过渡，对齐现 runtime） |
| CLI | `psync emit tools/psync/testdata/dsl/player.psync -o <out> --root tools/psync/testdata`（Go：`tools/psync`） |
| 对拍 | `psync.tests.test_emit_vs_meta` vs `build/examples/rpg_player/generated` |

**用法：**

```bash
psync emit tools/psync/testdata/dsl/player.psync -o /tmp/from-dsl --root tools/psync/testdata
go -C tools/psync test ./internal/emit/
```

**验收：** 路径 B 与路径 A **语义等价**（schema 字段合同、lua 结构、proto 消息）— 已绿。

---

### S3 — rpg 示例切到 DSL 源 ✅（过渡双轨）

**已交付：**

- CMake：`rpg_player_generate_from_dsl`（`psync emit` → `generated/from_dsl/`）  
- CMake：`rpg_player_dsl_check`（golden + emit≡Meta）  
- 头文件 / inch：**仍走 Meta**（下阶段再切 inch / `from_ir`）  
- 测试文档：[dsl-test.md](./dsl-test.md)

**验收：** `rpg_player_dsl_check`、`rpg_player_example`、`rpg_player_lua_replay` 绿。

---

### S4 — Lua runtime 线名对齐 ✅

**已交付：**

- `property_runtime.lua`：`list`≡`vector`、`dict`≡`map`  
- `psync emit --native-wire`：可直接输出 DSL 线名  
- 默认 emit 仍映射为 `vector`/`map`（对拍 Meta 不破）

**验收：** 旧测例对拍 PASS；native 产物由 `test_native_wire` 覆盖。

---

### S5 — Lua Record（标量 + array/list/dict）✅

**交付物：**

```text
meta/lua_runtime/property_record.lua   # 入队 + 改本地（flag 过滤对齐 C++）
tools/psync/emit.py → *_record.lua     # set_hp / tags_push / attrs_insert …
examples/rpg_player/lua_record_test.lua
CMake: rpg_player_lua_record
```

**API：**

```lua
local Record = require("property_record")
local Meta = require("Player_meta")
local rec = Record.bind(Meta)  -- need_flag_names 默认 sync_clients；Meta.FLAGS
rec.hp = 80
rec.pos:item_change(1, 3.5)
rec.tags:push("vip")
rec.attrs.atk = 100
local batch = rec:drain()  -- {offset,cmd,flag,data,offset_is_record=false}
```

**验收：** `rpg_player_lua_record` — Record→Replay 视图一致；mutate `flag`/`cmd`/`data` 形状对齐 C++ 约定。

---

### S6 — Lua Record（bag/slots/vec）✅

**已交付：**

- `property_record.lua`：`bag_*` / `slots_*` / `vec_*` + `item_change` 四元组  
- 语义对齐：slots 未 resize 静默；erase 按 id（bag）/ slot（slots）；嵌套 flag 用 item 字段 mask  
- `encode_item_pairs`：按 `need_flags` 过滤（`save_db` 字段不进 sync 队列）  
- 验收：`lua_record_test` 对 `lua_mutates.json` offsets 7–10 **23 条 deep_equal**

```bash
cmake --build build --target rpg_player_lua_record -j
```

---

### S7 — 交叉矩阵 ✅

| Record \ Replay | C++ Rep | Lua Rep |
|-----------------|---------|---------|
| C++ Rec | `rpg_player_example` | `rpg_player_lua_replay` |
| Lua Rec | `rpg_player_replay_json` + `lua_cross_matrix.lua` | `lua_record_test` / cross_matrix |

```bash
cmake --build build --target rpg_player_cross_matrix -j
```

---

### S8 — 文档与主路径 ✅

- [x] [core-principles.md](./core-principles.md)：合同源改为 DSL 双轨说明  
- [x] [evolution-plan.md](./evolution-plan.md) §10.1 探索主线修正  
- [x] [dsl-test.md](./dsl-test.md) 覆盖 S0–S7  
- libclang 路径**保留**（C++ inch）；未默认关闭（`WITH_LEGACY_META` 可后续再加）

---

## 4. 仓库改动面清单（实施时按此拆 PR）

| 目录/文件 | S1 | S2 | S3 | S4 | S5–S6 | S7–S8 |
|-----------|----|----|----|----|-------|-------|
| `tools/psync/testdata/dsl/*.psync` | 冻结 | | 依赖 | | | |
| `tools/psync/` 或 `scripts/psync/` | **新** | | | | | |
| `docs/ir-schema.json` | **新** | | | | | |
| `meta/generate_property_sync.cpp` | | IR 入口 | | | | 可选删 clang |
| `meta/mustache/*` | | 小改/兼容 | | | record 模板 | |
| `meta/lua_runtime/*` | | | | **改** | **新 record** | |
| `examples/rpg_player/*` | | | **CMake/头** | | 导出对拍 | harness |
| `test/` | golden IR | | | | | 交叉 |
| `docs/*` | 本文 | | | | | 收束 |

**建议 PR 粒度：** S1 单独合入；S2+S3 可串；S4 宜小 PR；S5 / S6 分开；S7 测试 PR。

---

## 5. 风险与缓冲

| 风险 | 应对 |
|------|------|
| Parser 工期膨胀 | 语法已极简；先支持样例所需子集，再补 `reserved`/`deprecated` |
| mustache 键与 `list`/`dict` 不一致 | S2 映射层；S4 双认 |
| Lua Record 与 C++ 边角不一致 | S6 以 C++ 队列 JSON 为金标，逐条修 |
| 双源（头文件 + DSL）漂移 | S3 尽快单一源；或 CI 对比 IR |
| 大整数 / json 类型 | 首版不做（dsl-types 已列后期） |

---

## 6. 明确本实施周期不做

- 客户端热更 sync  
- 纯 Lua 替换生产主服权威（只做沙盒/对拍）  
- P5 C++ Protobuf 编解码完整落地（可并行，不挡 DSL）  
- `omap` / `json` 字段 DSL  

---

## 7. 工具链约定（默认 Go）

- 仓库工具放在 [`tools/<name>/`](../tools/)：Go module + `cmd/<name>`，CMake `add_subdirectory` 产出二进制。
- 当前：[`tools/psync`](../tools/psync/) — `.psync` → IR / schema / lua / proto / C++（mustache）。
- 新工具优先 Go；不引入新的 Python 代码生成器。

## 8. 建议的立即下一步

1. ~~S0–S10~~ **已完成**（见 [dsl-test.md](./dsl-test.md)）。  
2. 可选后续：`rpg_player` inch 也切到 Go psync，去掉头文件 Meta 双源。  
3. 生产向：P5 PB 存档、P6 Lua→C++ 绑定（与纯 Lua Record 可选并存）。
