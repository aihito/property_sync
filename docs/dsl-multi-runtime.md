# DSL 驱动 · 多运行时 Record/Replay 方案梳理

> **探索命题：** 用 **DSL 定义属性**，生成代码；**C++ / Lua 均可作为 Record 端或 Replay 端**。  
> **本文角色：** 建议 + 架构梳理，**不落地实现**。与 [core-principles.md](./core-principles.md)、[lua-record.md](./lua-record.md)、[evolution-plan.md](./evolution-plan.md) 对照阅读。

---

## 1. 结论先说

你的方向 **比「C++ 头文件 Meta 为唯一真相源」更适合探索项目**，心智模型也更清晰：

```text
        属性 DSL（唯一真相源）
                 │
                 ▼
              中间 IR
                 │
     ┌───────┬───┴───┬───────┬────────┐
     ▼       ▼       ▼       ▼        ▼
  C++ Rec  C++ Rep  Lua Rec Lua Rep  schema/proto/…
```

**建议采纳的内核原则：**

1. **DSL 只描述「有什么字段 / 什么形状 / 谁能看见」**，不描述业务逻辑。  
2. **同步语义只有一份 IR 规范**（cmd / offset / flag / data 形状）；各语言 Record/Replay 都是 **emitter**，不是各自发明协议。  
3. **角色按进程配置，不按语言绑定**：同一份生成物里，某进程选 `Record` 或 `Replay`（或两者都链，但同一对象同一时刻只能有一个权威写者）。  
4. **用对拍锁死双实现**：DSL 场景脚本 → 各 Record 实现出队 → 各 Replay 吃队 → 视图相等。探索期这是「能不能做对称」的唯一可信证明。

**建议暂缓 / 警惕的：**

- 一上来就让「现网主服」改成纯 Lua Record（探索可做沙盒权威，主服仍可用 C++ Record）。  
- DSL 做成完整编程语言（有 if/循环/调用）——那是业务脚本，不是属性合同。  
- 手写第二套 Lua Record 却不从同一 IR 生成——必漂移。

相对 [lua-record.md](./lua-record.md) 里的 A/B/C：  
**DSL 多运行时 = 把「方案 B（纯 Lua Record）」升级为与 C++ 对等的一等公民**，并用生成 + 对拍管理风险；方案 A（绑定）变成可选优化（Lua 调 C++ Record），而不是唯一出路。

---

## 2. 为什么现在这条路更清晰

| 现状（Meta on C++） | DSL 多运行时 |
|--------------------|--------------|
| 真相源绑在 C++ 语法 + libclang | 真相源是语言无关的属性合同 |
| Lua Replay 是「跟班」生成物 | Lua / C++ 对称：都能 Rec 或 Rep |
| 「Lua 能否 Record」像后补问题 | 一开始就是矩阵四格都要有答案 |
| 扩展语言 = 再解析一种宿主语法 | 扩展语言 = 新写一个 emitter |

探索项目最需要的是：**协议与语义可讲清楚、可换宿主验证**。DSL 正好把「合同」从 C++ 注解里拔出来。

---

## 3. 目标架构

### 3.1 总图

```text
┌─────────────────────────────────────────────────────────┐
│  properties/*.psync（或 .yaml / .proto+注解）  DSL 源      │
└───────────────────────────┬─────────────────────────────┘
                            │ parse + validate
                            ▼
┌─────────────────────────────────────────────────────────┐
│  IR（内存/JSON）：Class / Field / wire_kind / index / flag │
│  + 兼容规则（只追加、禁复用 index…）                        │
└───────┬─────────────┬─────────────┬─────────────┬───────┘
        │             │             │             │
        ▼             ▼             ▼             ▼
   emit_cpp_rec  emit_cpp_rep  emit_lua_rec  emit_lua_rep
   emit_schema   emit_proto    emit_tests    emit_docs
                            │
                            ▼
              运行时矩阵（进程配置）
        ┌─────────────┬─────────────────┐
        │ Role=Record │ Role=Replay     │
        │ 改状态+入队  │ 只 apply_mutate │
        └─────────────┴─────────────────┘
              语言 ∈ {C++, Lua, …}
```

### 3.2 运行时矩阵（你要的「都可」）

|  | **Record（权威写）** | **Replay（镜像）** |
|--|----------------------|--------------------|
| **C++** | 已有 Proxy（继续生成） | 已有 replay_proxy |
| **Lua** | **新增生成**（入队 + 改 table） | 已有 runtime（补齐即可） |

合法部署例：

| 部署 | Record | Replay |
|------|--------|--------|
| 经典游戏服 | C++ | C++ 观察者 / Lua 客户端 |
| 脚本权威沙盒 | Lua | C++ 或 Lua 从节点 |
| 工具对拍 | 同 DSL 场景分别用 C++/Lua Record | 交叉 Replay |
| （不推荐）双写 | 同一实体两处 Record | — 必打架 |

**硬规则：** 每个实体实例 **同一时刻只有一个 Record 角色**；其它全是 Replay 或只读。

### 3.3 线格式仍共享

无论谁 Record，吐出的都是同一套：

```text
mutate_msg = { offset, cmd, flag, data }
snapshot   = encode_with_flag 同形 JSON / 日后 PB
```

这样「C++ Record → Lua Replay」与「Lua Record → C++ Replay」才能交叉验证。

---

## 4. DSL 建议长什么样

### 4.1 设计原则

| 要 | 不要 |
|----|------|
| 声明字段、类型、容器、flag、默认值 | 业务公式、技能逻辑、RPC |
| **稳定 index**（协议号）显式或锁定分配 | 每次按名字排序重排 index |
| wire_kind 与现库对齐：scalar/array/vector/map/bag/slots/vec | 无限自定义容器而不进 IR |
| 可被 diff（兼容 CI） | 隐式魔法字段 |

### 4.2 示意语法（探索用，非最终）

任选具体语法（YAML 上手快；自定义 `.psync` 更可控）。示意：

```yaml
# player.psync.yaml
version: 1
namespace: spiritsaway.rpg_example
flag_enum: rpg_property_flags   # 或 DSL 内嵌 flag 定义

class Player:
  schema_version: 1
  fields:
    - name: nickname
      index: 0
      type: string
      flags: [sync_clients]
      default: ""

    - name: hp
      index: 1
      type: int32
      flags: [sync_clients]
      default: 100

    - name: gold
      index: 3
      type: int32
      flags: [save_db]
      default: 0

    - name: inventory
      index: 7
      type: bag
      item: Item
      flags: [sync_clients, save_db]

class Item:
  kind: bag_item          # → 基类 id@0
  key_type: int32
  fields:
    - name: count
      index: 1
      type: int32
      flags: [sync_clients]
    - name: name
      index: 2
      type: string
      flags: [save_db]
```

要点：

- **index 写进 DSL**（或首次生成后冻结进 schema，禁止静默重排）。  
- `bag` / `slots` / `vec` 的 item 指向另一 class；生成器填 `item_meta` / proto import。  
- flag 名与位定义可在 DSL 顶部统一声明，避免 C++/Lua 各写一份。

### 4.3 DSL → IR

IR 建议就是现在生成器里的 `ClassModel` / `FieldModel` 升格为 **语言无关 JSON**（可落盘 `ir/Player.ir.json`），供：

- 多 emitter  
- `diff_schema`  
- 人类/CI 阅读  

现有 `generate_property_sync.cpp` 的 classify + ClassModel **可迁成「IR → mustache」**；前面加「DSL → IR」，后面加「Lua Record emitter」。

---

## 5. 生成物清单（对称）

| 产物 | Record | Replay |
|------|--------|--------|
| C++ | `*.proxy.inch` 等（现有） | `replay_mutate_msg`（现有） |
| Lua | `*_record.lua` + `property_record.lua` | `*_meta.lua` + `property_runtime.lua` |
| 合同 | `*.schema.json` | 同 |
| 线格式 | `*.proto` / MutateBatch | 同 |
| 测试 | 从 DSL 场景生成的 golden mutate 流 | 各 Runtime 消费 |

命名建议（探索清晰度优先）：

```text
generated/
  ir/Player.ir.json
  cpp/...
  lua/Player_record.lua
  lua/Player_meta.lua        # 属性元数据（原曾称 *_sync）
  lua/property_record.lua    # 手写/生成的入队引擎
  lua/property_runtime.lua   # Replay 引擎（可改名 property_replay.lua）
  schema/ proto/
```

---

## 6. Record / Replay 语义如何「一份规范、两边生成」

### 6.1 共享语义文档（应写成正式 spec）

对每个 `wire_kind` × `cmd` 规定：

- Record：改本地状态的副作用 + 入队 payload 精确形状  
- Replay：吃 payload 后的状态变换（应与 Record 后状态一致）  
- flag：何时入队 / encode 是否出现  

这份 spec 就是探索项目的「宪法」；C++/Lua 都是实现。

### 6.2 实现策略（推荐）

| 层 | 做法 |
|----|------|
| **算法核心** | 尽量用「可移植伪代码 / 共享测试向量」描述；语言各写一份，但测试同源 |
| **Lua Record** | 与 Replay **共用 table 形状**；Record = `mutate 本地 + push msg`（注意：不能简单「只 push 再 apply」，复杂容器在 apply 前可能有校验/静默丢弃，须按 C++ 行为对齐） |
| **C++ Record** | 保持现有 Proxy；长期可由 IR 生成，减少手写 mustache 分叉 |

**反模式：** Lua Record 只 `apply_mutate` 假装写完了、队列却与 C++ 不同形——交叉 Replay 会挂。

### 6.3 权威与一致性

探索期建议三种模式都支持，配置切换：

1. **Single-Record**：一进程 Record，其余 Replay（生产默认）。  
2. **Shadow**：Lua Record 与 C++ Record **并行跑同一 DSL 场景**，只比对队列，不双写实体（CI）。  
3. **Swap**：整服切到 Lua Record 沙盒，验证可行性。

---

## 7. 与现状的迁移关系

```text
阶段 0（现在）
  C++ 头文件 Meta ──libclang──► IR(隐式) ──► C++ Rec/Rep + Lua Rep

阶段 1（探索）
  DSL ──► IR(显式 JSON) ──► 同上 + Lua Rec（沙盒）
  （可选）仍允许从 C++ Meta「导出」DSL，降低改写成本

阶段 2
  DSL 为唯一手写源；C++ 头文件改为 #include 生成的声明，或极薄包装

阶段 3（可选）
  更多语言 emitter；PB Record/Replay；废除 libclang 依赖
```

**探索期务实路径：** 先 **DSL → IR → 现有 emitter + 新 Lua Record**，不必立刻删 Meta；可用「Meta 反生成 DSL」做迁移桥。

---

## 8. 风险与对策

| 风险 | 对策 |
|------|------|
| C++/Lua Record 行为漂移 | DSL 场景 → golden mutate；双边 Record 出队 deep_equal；再交叉 Replay |
| DSL 过度设计 | 先 YAML + 现有 wire_kind；不写表达式语言 |
| index 失控 | DSL 显式 index + schema diff CI |
| 性能（Lua Record 主服） | 探索用沙盒；生产默认 C++ Record，Lua 仅脚本通过队列/RPC |
| 与「无热更」冲突 | DSL/生成物仍同版本打包；换属性定义 = 发版 |
| slots/bag 边角（未 resize 等） | 语义 spec 逐条抄 C++ 测例；优先 rpg_player 全场景 |

---

## 9. 分阶段探索计划（建议）

| 阶段 | 交付 | 验收 |
|------|------|------|
| **D0** | DSL 设计 + `tools/psync/testdata/dsl/*.psync` | ✅ 已完成（见 dsl-design / dsl-types） |
| **D1+** | 实施 | 见 **[dsl-implementation-plan.md](./dsl-implementation-plan.md)**（S0–S8） |

不把「替换现网主服 Record」放进探索成功标准；成功标准是：**对称生成 + 交叉对拍成立**。

---

## 10. 对你思路的直接建议

1. **值得做，且应升格为探索主线**：比「先绑 C++ 再补 Lua Record」目标更干净。  
2. **先合同后实现**：IR + mutate 语义 spec + 对拍，再写 Lua Record。  
3. **语言对称 ≠ 部署对称**：生成上 C++/Lua 都能 Rec/Rep；部署上默认仍 **单 Record**。  
4. **继承现有资产**：Replay 引擎、rpg_player 场景、schema/proto、生成器 ClassModel 都是 IR/emitter 的雏形，不要推倒重来。  
5. **和 lua-record.md 的关系**：A（绑定）降为加速器；B（纯 Lua Record）变为 **DSL 生成的正式产物**；C（伪 Record）仅作 D3 前的临时梯子。

---

## 11. 一句话

把真相源从「C++ 注解」换成 **属性 DSL → 共享 IR**，再对 C++/Lua **对称生成 Record 与 Replay**；用 **交叉对拍** 保证只有一份同步语义。探索成功看矩阵是否跑通，而不是看谁当主服权威。

---

## 相关文档

| 文档 | 关系 |
|------|------|
| [core-principles.md](./core-principles.md) | 现行 Record/Replay 原理（实现仍多为 C++ Rec） |
| [lua-record.md](./lua-record.md) | 仅讨论「Lua 如何写」的三条路；本文将其纳入 DSL 对称图景 |
| [evolution-plan.md](./evolution-plan.md) | 早期「权威留 C++」；探索主线可按本文修正目标陈述 |
| [lua-sync.md](./lua-sync.md) / [compatibility.md](./compatibility.md) | Replay / 兼容仍适用 |
