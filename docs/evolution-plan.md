# property_sync 演进完整方案

> 覆盖：字段兼容、Protobuf 存档/线格式、跨语言（纯 Lua Replay）、与现有 C++ Proxy 的关系。  
> 约束：**生成的 Lua 为纯 Lua**；**客户端不支持热更**（同步代码随客户端版本打包，不在运行时热补丁）。

---

## 1. 背景与目标

### 1.1 现状

| 能力 | 现状 |
|------|------|
| 增量同步 | C++ `prop_record_proxy` Record → `mutate_msg` → `prop_replay_proxy` Replay |
| 载荷 | JSON（any_container） |
| 路径 | `uint64` offset + 字段 `index`（≤255/层） |
| 跨语言 | 无官方 schema / IDL / 非 C++ Replay |
| 兼容 | 靠开发者自觉；删改字段易导致静默错档 |

### 1.2 目标

1. **兼容可治理**：字段演进有明确规则 + 可自动 diff 的 schema。  
2. **Protobuf 可生成**：全量 Snapshot / 增量 Mutate 有 IDL，存档可读、跨语言通用。  
3. **纯 Lua 可 Replay**：客户端/脚本用生成的纯 Lua 消化同一套增量；**不依赖 FFI/C 模块做 Replay**（解码 PB 可用宿主已有能力或先走 JSON）。  
4. **无热更假设**：Lua 生成物与 C++、proto **同一客户端版本编译进包**；改属性定义 = 发新版本，不做运行时热更同步逻辑。

### 1.3 非目标（本期不做）

- 用 Lua 替换 C++ 成为权威属性源  
- 客户端热更 inch / Lua sync / proto  
- 一次改掉现网 JSON 队列（允许双轨过渡）

---

## 2. 总体架构

```text
                    Meta(property)  C++ 定义（唯一真相源）
                                  │
                    generate_property_sync（构建期）
                                  │
        ┌─────────────┬───────────┼───────────┬─────────────┐
        ▼             ▼           ▼           ▼             ▼
   *.proxy.inch  *.schema.json  *.proto  *_sync.lua   兼容报告/CI
   (C++ Record)   (合同)      (线格式)  (纯Lua Replay)
        │             │           │           │
        ▼             └─────┬─────┴─────┬─────┘
  服务端 C++ 权威            │           │
  Record → Mutate ──────────┼───────────┤
        │                   │           │
        ▼                   ▼           ▼
   网络 / 存盘          其它语言      客户端纯 Lua
   (JSON 或 PB)        (可选)        apply_mutate / load_snapshot
```

**权威仍在 C++（或服务端）**；Lua 侧默认是 **镜像 Replay + 读**。服务端若用 Lua 写业务，通过绑定调 C++ Proxy（可选后期），不把 Record 语义再实现一套权威。

**无热更含义：**

- 构建流水线：改 `Player` → 跑 meta → 产出 inch / schema / proto / `Player_sync.lua` → **打进同一 Client/Server 包**。  
- 旧客户端不加载新 Lua sync；协议不兼容靠 **版本号拒绝** 或强制更新，不做热更补丁缝合。

---

## 3. 字段兼容方案

### 3.1 硬性规则

| 规则 | 说明 |
|------|------|
| R1 只追加 | 新字段只能加在类定义末尾，使用新 `index` |
| R2 不回收编号 | 废弃字段不得删除后让后面字段前移；保留占位或 `reserved` |
| R3 不改语义 | 同 index 不得改类型/容器种类（标量↔bag、bag↔slots 等） |
| R4 版本号 | 存档与连接握手带 `schema_version`；不兼容则拒载/拒连 |
| R5 双端同版 | 无热更客户端：C++ / Lua / proto **同版本产物**，禁止「只热更脚本不同步 schema」 |

### 3.2 `schema.json`（跨语言合同）

每个属性类生成一份，例如 `Player.schema.json`：

```json
{
  "class": "Player",
  "schema_version": 3,
  "namespace": "spiritsaway::rpg_example",
  "fields": [
    {
      "index": 1,
      "name": "nickname",
      "cpp_type": "std::string",
      "wire_kind": "string",
      "flags": ["sync_clients"]
    },
    {
      "index": 8,
      "name": "inventory",
      "wire_kind": "bag",
      "item_class": "Item",
      "flags": ["save_db", "sync_clients"]
    }
  ]
}
```

CI：`diff_schema(old, new)`  

- 允许：新增更大 index、`schema_version` 递增说明  
- 失败：删除 index、同 index 改名/改 `wire_kind`、index 重排  

### 3.3 与 Protobuf / Lua 对齐

- proto `field number` **=** property `index`（或统一偏移，全项目固定一种）  
- 废弃：`reserved <index>;` + schema 标记 `"deprecated": true`  
- Lua 生成代码按 **同一 index** 写 `if field == N`，无热更则旧包不会混用新 index 表  

### 3.4 文档与示例

- 新增 `docs/compatibility.md`（规则 + 反例）  
- 示例 `Player` 增加可读的 `schema_version` 字段或存档头字段（二选一，推荐**存档/包头**，避免占用业务 index 争论）

---

## 4. Protobuf 方案

### 4.1 生成内容

**A. Snapshot（全量）** — 进视野、落盘、Lua `load_snapshot`

```protobuf
syntax = "proto3";
package property_sync.rpg;

message ItemSnapshot {
  int32 id = 1;      // bag 键可冗余一份便于阅读
  int32 count = 2;   // = Item::index_for_count
  string name = 3;
}

message PlayerSnapshot {
  uint32 schema_version = 1;
  string nickname = 2;
  int32 hp = 3;
  // ...
  repeated ItemSnapshot inventory = 8;
}
```

**B. Mutate（增量）** — 与现 `mutate_msg` 对齐

```protobuf
enum PropertyCmd {
  CMD_CLEAR = 0;
  CMD_SET = 1;
  // ... 与 property_cmd 数值一致
}

message MutateMsg {
  uint64 offset = 1;
  PropertyCmd cmd = 2;
  uint64 flag = 3;
  bytes data = 4;   // v1：与现 JSON 载荷二进制/UTF-8 JSON 等价，便于过渡
}

message MutateBatch {
  uint32 schema_version = 1;
  repeated MutateMsg msgs = 2;
}
```

### 4.2 分期

| 阶段 | 内容 |
|------|------|
| PB-1 | Meta 只生成 `.proto` + 文档字段号约定 |
| PB-2 | C++：Snapshot ↔ 现有 `encode_with_flag` 对拍；存档演示 |
| PB-3 | 增量 `MutateBatch` 上网；`data` 仍为 JSON bytes |
| PB-4（可选） | 常用 cmd 改为 typed `oneof`，进一步去掉 JSON |

### 4.3 存档可读性

- 工具链：`protoc --decode` / 示例打印 `DebugString`  
- 运营/QA：看 Snapshot 字段名；看 MutateBatch 的 cmd + offset 对照 schema.json  

### 4.4 依赖

- **生成 `.proto`**：零运行时依赖  
- **编解码**：仅 Server/工具链/需要的客户端链 `protobuf`；纯 Lua Replay **不强制**链 C protobuf（见下节）

---

## 5. 纯 Lua 跨语言方案（无热更）

### 5.1 定位

| 项 | 决定 |
|----|------|
| 代码形态 | **纯 Lua 5.x**（或项目指定版本），无 `require` C 写的 replay 核心 |
| 发布 | 生成的 `*_sync.lua` **打进客户端包**，与该版本 schema/proto 锁定 |
| 热更 | **不支持**：不提供「只更新 lua sync、不更新 native」；版本不一致则拒绝同步 |
| 权威 | Replay 镜像；写属性走服务端/C++（或后期绑定，非本期纯 Lua Record） |

### 5.2 生成物（纯 Lua）

```text
generated/lua/
  property_cmd.lua       -- 与 C++ enum 数值一致
  property_offset.lua    -- split/merge 与 C++ 约定一致（纯 Lua 位运算）
  Item_sync.lua
  Buff_sync.lua
  Player_sync.lua        -- load_snapshot / apply_mutate / new_default
  schema_version.lua     -- 本包锁定的版本常量
```

`Player_sync.lua` 示意：

```lua
local M = {}
M.SCHEMA_VERSION = 3  -- 与包内 schema 锁定

function M.new_default()
  return {
    nickname = "",
    hp = 100,
    inventory = {},  -- [id] = { count=..., name=... }
    -- ...
  }
end

function M.load_snapshot(player, snap)
  -- snap 已是 Lua table（由 JSON 或 PB→table 得到）
  assert(snap.schema_version == M.SCHEMA_VERSION, "schema mismatch; update client")
  player.nickname = snap.nickname or ""
  -- ...
end

function M.apply_mutate(player, msg)
  -- msg = { offset=..., cmd=..., flag=..., data=... } 已是 table
  local path = offset.split(msg.offset)
  -- 按 path[1] 分发到字段；bag 再处理 item_change
end

return M
```

**纯 Lua 含义：** `apply_mutate` / `load_snapshot` / offset 解析 **全部 Lua 实现**。  
宿主仅需提供：把网络字节变成 Lua table（`cjson` 或已有 `pb` 解码库）。若客户端已有 pb 库，只负责 decode；**Replay 逻辑仍是生成的纯 Lua**。

### 5.3 与无热更的版本门闩

```text
连接或加载存档:
  if packet.schema_version != Client.SCHEMA_VERSION then
      拒绝对拍 / 提示更新客户端
  end
```

禁止：热更单独替换 `Player_sync.lua` 而不换 native/proto。流程上 Lua 生成进 **同一构建号制品**。

### 5.4 bag / slots / vec 在 Lua table 中的形状

| C++ | Lua 镜像 |
|-----|----------|
| `property_bag` | `{ items = {…}, id_to_idx = { [id]=1-based } }` |
| `property_slots` | `{ size = N, by_slot = { [slot]=item }, by_id = { [id]=item } }` |
| `property_vec` | 数组部分 `t[1..n]`（Lua 1-based；mutate 下标仍按 C++ 0-based） |

`encode_sync_view` 输出与 C++ `encode_with_flag(sync_clients, …)` 同形（bag/vec 为对象数组；slots 为 `{sz,data}`）。

`item_change` / `slot_swap` 等与 C++ **同 cmd 语义**，由 `property_runtime.lua` 实现；生成模块只提供元数据。

### 5.5 对拍验收（必须做）

1. C++ `rpg_player_example` Record → 导出 `MutateBatch`（JSON 或 PB）。  
2. 纯 Lua 测试（命令行 `lua` 或嵌入）`apply_mutate` 全序列。  
3. 导出 Lua 侧 `sync_clients` 视图 JSON，与 C++ `encode_with_flag` **逐字段相等**。  

无热更环境下，对拍在 **CI 同版本产物** 上跑即可。

### 5.6 可选后期：服务端 Lua 写属性

- 不生成「纯 Lua Record 权威」，避免第二套实现。  
- 用绑定：`player.hp = 80` → C++ `prop_record_proxy`（非纯 Lua，属 native 插件）。  
- 与「客户端纯 Lua Replay」分离，文档写清。

---

## 6. 构建与发布流水线（无热更）

```text
开发改 rpg_player.h / Item
        │
        ▼
CI / 本地: generate_property_sync
        │
        ├─ C++ inch/proxy
        ├─ schema.json
        ├─ .proto
        └─ pure Lua *_sync.lua
        │
        ▼
schema diff vs 上一发布标签 ──失败则阻断
        │
        ▼
编译 C++ Server/Client + 打包 Lua + 嵌入 schema_version
        │
        ▼
发版（整包更新；客户端无热更通道更新 sync 脚本）
```

`config.json` 扩展示例：

```json
{
  "generated_folder": "./generated",
  "schema_folder": "./generated/schema",
  "proto_folder": "./generated/proto",
  "lua_folder": "./generated/lua",
  "lua_version": "5.4",
  "emit_schema": true,
  "emit_proto": true,
  "emit_lua": true
}
```

---

## 7. 实施分期与 PR 划分

| 阶段 | 交付 | 验收 |
|------|------|------|
| **P0** | `docs/compatibility.md` + 本方案入 `docs/` | 规则评审通过 |
| **P1** | Meta 生成 `*.schema.json` + `diff_schema` 脚本 | 故意删字段 CI 失败 |
| **P2** | Meta 生成 Snapshot/Mutate `.proto` | 字段号 = index；示例 proto 可 `protoc` |
| **P3** | 纯 Lua：`property_cmd` + offset + `Player_sync.apply_mutate`（吃 JSON msg） | 与 C++ example 对拍 PASS |
| **P4** | Snapshot JSON/PB 加载进 Lua `load_snapshot` | 全量+增量混合对拍 |
| **P5** | C++ 存档 PB、MutateBatch 可选上网 | 存档可读；旧 JSON 开关仍可用 |
| **P6** | （可选）typed mutate oneof；服务端 Lua 绑定 Record | 按需 |

建议优先：**P0 → P1 → P3（Lua JSON 对拍）→ P2/P4/P5**。  
原因：无热更客户端最需要「纯 Lua 能跟 C++ 对齐」；schema 合同与 Lua 可并行，proto 可紧随。

---

## 8. 风险与对策

| 风险 | 对策 |
|------|------|
| Lua 与 C++ Replay 行为漂移 | 生成同源 + CI 对拍；禁止手改生成 Lua |
| 无热更导致改字段必须发版 | 接受；用只追加降低发版痛苦；大改走 version bump |
| PB `data` 仍是 JSON | 过渡期明确；P5/P6 再收紧 |
| Lua 1-based vs C++ 0-based | 生成层统一转换，测试覆盖 vec/slots |
| 包体变大 | Lua 生成按类拆分；客户端可只打进需要的类 |

---

## 9. 成功标准（总）

1. **兼容**：违规改字段能被 schema diff 拦住；文档规则可执行。  
2. **Proto**：每个属性类有 Snapshot；Mutate 与现 cmd/offset 对齐；存档可解码阅读。  
3. **纯 Lua**：零 C 依赖完成 Replay；与同版本 C++ 镜像对拍一致。  
4. **无热更**：`SCHEMA_VERSION` 门闩；制品清单含 C++/Lua/proto/schema 同一构建号。

---

## 10. 文档与代码落点

| 路径 | 用途 |
|------|------|
| `docs/evolution-plan.md` | 本文 |
| `docs/compatibility.md` | 兼容细则（已落地） |
| `docs/protobuf.md` / `docs/lua-sync.md` | PB / Lua 约定（已落地） |
| `meta/mustache/property_schema.mustache` 等 | schema / proto / lua 模板（已接入生成器） |
| `scripts/diff_schema.py` | schema 破坏性变更检测 |
| `generated/schema|proto|lua/` | 构建期输出目录 |
| `examples/rpg_player/` | C++ 示例；`lua_replay.lua` 纯 Lua 对拍 |

**当前进度：** P0–P4 已落地（含 `rpg_player_proto_check` 的 `protoc` 验收，以及 Lua batch / snapshot / mixed 对拍）。P5 C++ PB 存档与 P6 typed mutate / 服务端 Lua 绑定仍待做。

### 10.1 探索主线修正（DSL 多运行时）

原默认「权威 Record 仅 C++、Lua 只 Replay」仍然是**生产主服**建议。探索上已验证另一条对称路径（详见 [dsl-multi-runtime.md](./dsl-multi-runtime.md)、[dsl-implementation-plan.md](./dsl-implementation-plan.md)）：

| 能力 | 状态 |
|------|------|
| `.psync` → IR → schema/lua/proto | S0–S3 已落地（与 Meta 产物语义对拍） |
| Lua runtime 认 `list`/`dict` | S4 |
| 纯 Lua Record（含 bag/slots/vec） | S5–S6；与 C++ 队列 deep_equal |
| Record×Replay 交叉矩阵 | S7（`rpg_player_cross_matrix`） |

**含义：** 工具链 / 脚本服 / 对拍沙盒可用纯 Lua Record；主服若继续 C++ 权威，仍走 Proxy，DSL 作合同源即可。P6b（Lua→C++ 绑定）与纯 Lua Record **并存可选**，不是互斥替代。

---

## 11. 一句话总结

以 **`.psync` DSL / IR 为合同真相源**（C++ inch 可暂留 Meta 双轨），构建期产出 **C++ Proxy、schema、Protobuf IDL、纯 Lua Replay/Record**；用 **schema_version + 整包发版** 替代热更；主服权威可仍在 C++，Lua 可做同版本镜像或对称 Record（经交叉矩阵锁语义）。
