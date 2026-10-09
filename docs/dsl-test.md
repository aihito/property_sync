# DSL / IR / Emit 测试文档

> 覆盖 S0–S4：`.psync` → IR → schema/lua/proto，以及与 Meta 双轨对拍、Lua runtime `list`/`dict` 兼容。  
> 仓库根目录记为 `$REPO`（下文命令均在 `$REPO` 下执行）。

---

## 1. 测试地图

| 层级 | 验证什么 | 命令 / 目标 | 依赖 |
|------|----------|-------------|------|
| **T1 词法语法** | `tools/psync/testdata/dsl/*.psync` 可解析 | `psync check` | Go |
| **T2 IR golden** | 编译 IR ≡ `tools/psync/testdata/ir` | `go test ./internal/compile` | Go |
| **T3 Emit 语义** | DSL emit ≡ Meta/generated inch | `go test ./internal/emit` | Go + 已有 inch |
| **T4 Native wire** | `--native-wire` 出 `list`/`dict` | `psync emit --native-wire` | Go |
| **T5 C++ 示例** | Record 观察者一致 | `rpg_player_example` | CMake + Meta |
| **T6 Lua 对拍** | batch / snapshot / mixed | `lua_record_replay` | Lua + DSL |
| **T7 Proto** | protoc 可编译生成物 | `rpg_player_proto_check` | protoc |
| **T8 单元测试** | 核心属性库 | `property_test` | 见 build-and-test |
| **T9 Lua Record** | 纯 Lua 写属性入队 | `lua_record_test` | Lua + DSL emit |
| **T10 交叉矩阵** | Record×Replay 四格 | `lua_record_cross` | `examples/lua_record` |

一键 DSL 层（T1–T4）：

```bash
chmod +x tools/run_dsl_tests.sh   # 首次
./tools/run_dsl_tests.sh
# 或：cmake --build build --target psync_check -j
```

全仓验收（T1–T10 + 示例，含 C++）：

```bash
cmake --build build --target check_all -j"$(nproc)"
```

---

## 2. 环境准备

### 2.1 仅 DSL / IR（无 C++）

```bash
cd "$REPO"
go -C tools/psync build -o /tmp/psync ./cmd/psync
/tmp/psync version   # 期望打印 psync 0.2.0
```

### 2.2 完整对拍（含 Meta / C++ / Lua）

按 [build-and-test.md](./build-and-test.md) 配置并编译：

```bash
export DEPS=/home/game/open-source/game-server/_deps/install   # 按本机调整
cmake -S . -B build \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_PREFIX_PATH="$DEPS;$DEPS/lib64/cmake;/usr/lib64/llvm21/lib64/cmake" \
  -DClang_DIR=/usr/lib64/llvm21/lib64/cmake/clang \
  -DLLVM_DIR=/usr/lib64/llvm21/lib64/cmake/llvm \
  -DWITH_TEST=ON \
  -DWITH_EXAMPLES=ON

cmake --build build --target rpg_player_generate -j"$(nproc)"
```

产物目录：`build/examples/rpg_player/generated/{schema,lua,proto,…}`。

---

## 3. 分项操作与判定

### 3.1 T1 — 解析校验

```bash
export PYTHONPATH="$REPO/tools"
psync check tools/psync/testdata/dsl/player.psync --root tools/psync/testdata
# 期望：ok: …/dsl/player.psync
```

失败时 stderr 打印 `ERROR [V*]`（见 `docs/dsl-design.md` 校验规则）。

### 3.2 T2 — IR Golden

```bash
python3 -m unittest psync.tests.test_golden -v
# 期望：OK（2 tests）
```

刷新 golden（**仅在有意改 DSL/IR 合同后**）：

```bash
psync compile tools/psync/testdata/dsl/player.psync -o tools/psync/testdata/ir --root tools/psync/testdata --no-bundle
```

### 3.3 T3 — DSL Emit ≡ Meta

前置：已执行 `rpg_player_generate`。

```bash
python3 -m unittest psync.tests.test_emit_vs_meta -v
# 期望：OK（3 tests：schema / lua / proto 语义）
```

或 CMake：

```bash
cmake --build build --target rpg_player_generate_from_dsl -j
cmake --build build --target rpg_player_dsl_check -j
```

- DSL 旁路输出：`build/examples/rpg_player/generated/from_dsl/{schema,lua,proto}`
- 比较忽略 Meta 的 `cpp_type` 拼写噪音（如 `basic_string`）与 mustache 空行

手动 emit：

```bash
psync emit tools/psync/testdata/dsl/player.psync -o /tmp/from-dsl --root .
# 默认：list→vector、dict→map（与现 Meta / 旧测例对齐）
```

### 3.4 T4 — Native wire（S4）

```bash
psync emit tools/psync/testdata/dsl/player.psync -o /tmp/native --root . --native-wire
# Player.schema.json 中 tags.wire_kind == "list"，attrs == "dict"
```

`meta/lua_runtime/property_runtime.lua` 已同时认 `list`/`dict` 与 `vector`/`map`，native 产物可直接给 runtime 用。

### 3.5 T5 — C++ RPG 示例

```bash
cmake --build build --target rpg_player_example -j
./build/examples/rpg_player/rpg_player_example
# 期望：退出码 0，日志含 [PASS]
```

### 3.6 T6 — C++/Lua 对拍

```bash
cmake --build build --target rpg_player_lua_replay -j
# 期望：三条路径（batch / snapshot / mixed）均 PASS
```

> 若刚改了 `property_runtime.lua`，请先 `rm -rf build/examples/rpg_player/generated` 再 `rpg_player_generate`，确保 runtime 被拷到生成目录。

### 3.7 T7 — Proto

```bash
cmake --build build --target rpg_player_proto_check -j
# 有 protoc 时生成 descriptor；无 protoc 则目标不存在（可跳过）
```

### 3.8 T8 — property_test

见 [build-and-test.md §4](./build-and-test.md)。期望：无 `fail to relay`，退出码 `0`。

### 3.9 T9 — Lua Record（S5 + S6）

```bash
# 手动
PYTHONPATH=tools python3 -m psync emit tools/psync/testdata/dsl/player.psync -o /tmp/from-dsl --root .
# 先跑 example 生成 fixtures/lua_mutates.json（若无）
cmake --build build --target rpg_player_example_run
lua examples/lua_record/lua/lua_record_test.lua /tmp/from-dsl/lua \
  examples/rpg_player/fixtures/lua_mutates.json

# 或 CMake
cmake --build build --target rpg_player_lua_record -j
```

期望：

- `[PASS] S5 Lua Record self roundtrip`
- `[PASS] S6 Lua Record self roundtrip`
- `[PASS] S6 Lua Record mutates deep_equal C++ (23 msgs, offsets 7-10)`

覆盖：S5 标量/array/list/dict；S6 bag/slots/vec（含未 resize 静默、`item_change` 信封、`save_db` 不入队）。

### 3.10 T10 — 交叉矩阵（S7）

| Record \ Replay | C++ Rep | Lua Rep |
|-----------------|---------|---------|
| C++ Rec | `rpg_player_example` | `lua_replay --batch` |
| Lua Rec | `rpg_player_replay_json` | `lua_cross_matrix` 自洽 |

```bash
cmake --build build --target rpg_player_cross_matrix -j
# 期望矩阵四格均为 ok/PASS，含 [PASS] LuaRec→CppRep
```

---

## 4. 推荐回归顺序（发版前）

```bash
# A. 纯 Python（秒级）
./tools/run_dsl_tests.sh

# B. Meta 生成 + 对拍（需已 configure）
cmake --build build --target rpg_player_generate -j
cmake --build build --target rpg_player_dsl_check -j

# C. 端到端 + 交叉矩阵（推荐一条龙）
cmake --build build --target rpg_player_cross_matrix -j
# 或分步：
cmake --build build --target rpg_player_lua_replay rpg_player_lua_record -j
cmake --build build --target rpg_player_proto_check -j   # 可选
```

---

## 5. CMake 目标速查（DSL 相关）

| 目标 | 作用 |
|------|------|
| `rpg_player_generate` | Meta（libclang）→ inch + schema/lua/proto |
| `rpg_player_generate_from_dsl` | `psync emit` → `generated/from_dsl/` |
| `rpg_player_dsl_check` | unittest：golden + emit≡Meta |
| `rpg_player_lua_replay` | C++ 导出 mutate + Lua Replay 对拍 |
| `rpg_player_lua_record` | 纯 Lua Record（S5/S6）自洽 + mutate deep_equal |
| `rpg_player_replay_json` | C++ 可执行：mutate JSON → sync view |
| `rpg_player_cross_matrix` | S7 交叉矩阵一条龙 |
| `rpg_player_proto_check` | protoc 编译检查 |

当前双轨：

```text
路径 A：头文件 Meta ──► inch（C++ Record）+ schema/lua/proto
路径 B：tools/psync/testdata/dsl/*.psync ──► IR ──► schema/lua/proto（from_dsl）
         CI：路径 B ≡ 路径 A（语义）
```

C++ inch 仍走 Meta；DSL 为 schema/lua/proto **合同源**（与头文件需人工保持一致，S3 后续可再切 inch）。

---

## 6. 故障排查

| 现象 | 处理 |
|------|------|
| `ModuleNotFoundError: psync` | `export PYTHONPATH=$REPO/tools` |
| `emit vs Meta` Skip / fail 缺文件 | 先 `rpg_player_generate` |
| Lua 对拍 FAIL 且刚改 runtime | 清 `generated/` 后重生 |
| golden 失败 | 确认是否故意改了 DSL；是则重刷 `testdata/ir` |
| `rpg_player_generate_from_dsl` 无目标 | CMake 未找到 Python3；重新 configure |

---

## 7. 相关文档

| 文档 | 内容 |
|------|------|
| [dsl-design.md](./dsl-design.md) | `.psync` 语法 |
| [dsl-types.md](./dsl-types.md) | 类型矩阵 |
| [dsl-implementation-plan.md](./dsl-implementation-plan.md) | S0–S8 看板 |
| [ir-schema.json](./ir-schema.json) | IR JSON Schema |
| [build-and-test.md](./build-and-test.md) | C++/依赖/通用编译 |
| [lua-sync.md](./lua-sync.md) | Lua Replay 约定 |
