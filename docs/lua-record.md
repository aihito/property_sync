# Lua Record 方案梳理

> **问题：** 当前权威 **Record**（改内存 + 入队）只在 C++ `prop_record_proxy`；纯 Lua 已具备 **Replay**（`apply_mutate` / `load_snapshot`）。能否、以及如何让 Lua 也能「写属性」？  
> **本文目标：** 先定方案与边界，**不落地实现**。与 [evolution-plan.md §5.6](./evolution-plan.md)、[lua-sync.md](./lua-sync.md)、[core-principles.md](./core-principles.md) 配套。

---

## 1. 现状（事实）

| 侧 | 语言 | 能力 |
|----|------|------|
| Record（权威写） | **仅 C++** | `prop_record_proxy`：改对象 + 写 `mutate_msg` |
| Replay（镜像读/回放） | C++ + **纯 Lua** | `prop_replay_proxy` / `Player_meta.apply_mutate` |

因此：

- 客户端 / 工具用纯 Lua **跟同步**已经够用（P3/P4 已对拍）。  
- 若 **服务端业务想用 Lua 写** `player.hp = 80`，今天必须绕到 C++，或根本写不了权威状态。

演进方案里原结论是：**不要做第二套纯 Lua 权威 Record**；优先「Lua 调 C++ Proxy」。下面把可选路径摊开，方便按场景选型。

---

## 2. 先分清三种「写」

不要把下面三件事混成一件：

| 代号 | 含义 | 权威在哪 | 产出 mutate？ |
|------|------|----------|---------------|
| **W1 镜像写** | 本地改 table，不同步 | 无（或仅 UI） | 否 |
| **W2 记账写（Record）** | 改权威状态 + 产生与 C++ 同形的 `mutate_msg` | 必须唯一 | 是 |
| **W3 请求写** | Lua 发 RPC，由 C++ Record 执行 | 仍在 C++ | 由 C++ 产生 |

本库关心的是 **W2**。W3 是业务框架层，不必进 property_sync 内核。

---

## 3. 方案对比

### 方案 A：Lua → C++ 绑定（推荐默认）

```text
Lua: player:hp_set(80) 或 player.hp = 80（__newindex）
        │
        ▼  sol2 / LuaBridge / 手写 userdata
C++: prop_record_proxy<Player>::hp().set(80)
        │
        ▼
top_msg_queue  ← 与现网完全同一条路径
```

| 项 | 说明 |
|----|------|
| 权威 | **仍只有 C++ 一份** |
| mutate 语义 | 零分叉：队列、flag、item_change 全是现成实现 |
| Lua 形态 | **非纯 Lua**（依赖 native 模块）；与客户端纯 Lua Replay **故意分离** |
| Meta | 可生成薄绑定桩（`hp_set` / 容器方法表），或通用 `record_call(path, cmd, data)` |
| 工作量 | 中：绑定层 + 生成 API；内核几乎不动 |
| 风险 | 绑定生命周期（Player userdata 与 C++ 对象同寿）；错用绕过 Proxy |

**适用：** 服务端脚本（任务、GM、活动）写属性；权威必须与现有 C++ 服一致。

**对应规划：** evolution P6b。

---

### 方案 B：纯 Lua Record（第二套实现）

```text
Lua: PlayerRecord:hp_set(80)
        │
        ├─ 改本地 table
        └─ 拼 mutate_msg 入 Lua 队列
        │
        ▼
与 C++ Record 对拍 / 或 Lua 权威服
```

| 项 | 说明 |
|----|------|
| 权威 | Lua 也可成权威（独立脚本服、单机、工具） |
| 本质 | 把 `prop_record_proxy` + bag/slots/vec 入队逻辑 **再实现一遍** |
| 与现有 Replay | 可复用 `property_runtime` 的 table 形状；**入队规则要新写** |
| 工作量 | **大**：每个 wire_kind 的 Record 语义、flag 过滤、item 信封、slots 下标约定 |
| 风险 | **双实现漂移**（C++ 改一处，Lua 漏一处）；对拍成本永久存在 |

**适用：** 明确要「无 C++ 的权威进程」（例如纯脚本 DS、编辑器模拟），且愿意用 CI 对拍锁死语义。

**不推荐**作为当前主服路径的默认选择——与演进方案「避免第二权威」一致。

若做，建议硬约束：

1. Meta **同源生成** Record API（禁止手写业务侧入队）。  
2. CI：**同一操作序列** → C++ 队列 JSON ≡ Lua 队列 JSON（比 Replay 对拍更严）。  
3. 版本与 `SCHEMA_VERSION` 同包；仍无热更。

---

### 方案 C：Lua「伪 Record」= 自产 mutate + 本地 Replay

```text
Lua 业务想改 hp
        │
        ▼
构造 mutate_msg{ offset=INDEX.hp, cmd=set, data=80 }
        │
        ├─ push 到发送队列（给别人）
        └─ apply_mutate(自己)     ← 已有 Replay
```

| 项 | 说明 |
|----|------|
| 权威 | 「谁有权构造合法 mutate」——若只有本进程，则本进程权威；**没有 C++ 校验** |
| 复用 | 最大化复用现有 `apply_mutate`；**不必**实现完整 Proxy 树 |
| 缺口 | 复杂写（`inventory.get(id).count.set`）要手拼 `item_change` 载荷；易错 |
| 风险 | 绕过 C++ 规则（非法 slot、未 resize 就 insert 等）只能靠 Lua 再实现校验 |

**适用：** 工具链、单机预览、测试夹具；或「写操作极少且全是标量」的脚本。

可演进为：Meta 生成 `record_set(obj, field, value)` 等糖，内部仍是「组 msg + apply」；容器级再逐步加糖。这是 **B 的渐进子集**，不是替代 A。

---

## 4. 决策建议

```text
服务端权威仍在 C++，只是想用 Lua 写业务？
        │
        ├─ 是 → 【方案 A】绑定（默认）
        │
        └─ 否 → 需要无 C++ 的权威进程？
                  │
                  ├─ 是，且接受双实现 + 强对拍 → 【方案 B】
                  │
                  └─ 仅工具/预览/简单标量 → 【方案 C】可先做
```

| 场景 | 推荐 |
|------|------|
| 现网游戏服 + Lua 脚本改属性 | **A** |
| 客户端表现 / 跟同步 | 维持现有 **纯 Lua Replay**（不写 Record） |
| 纯 Lua 独立权威服 | **B**（立项前先估对拍成本） |
| 编辑器 / 单机沙盒 | **C** → 不够再升 B 或接 A |

---

## 5. 若走方案 A：落地轮廓（仍属设计）

### 5.1 API 形态（二选一或并存）

**显式（生成、好查）：**

```lua
local proxy = PlayerRecord.bind(cpp_player)  -- userdata
proxy:set_hp(80)
proxy:inventory():get(1001):set_count(5)
```

**糖（`__newindex`）：**

```lua
proxy.hp = 80   -- 映射到 set
-- 容器仍建议显式方法，避免 Lua 表语义与 bag 冲突
```

### 5.2 Meta 生成物（草案）

```text
generated/lua/
  Player_record_bind.md   或
  Player_record.lua       -- 纯文档 + 调用约定
C++:
  Player_lua_binding.inc  -- sol2 usertype 注册（可选）
```

绑定实现放 **服务端插件**，**不要**打进客户端纯 Lua 包（避免客户端误以为可本地权威写）。

### 5.3 验收

1. 同一脚本序列：Lua 绑定写 vs 纯 C++ Proxy 写 → `mutate_msg` 队列逐条相等。  
2. 下游仍用现有 Lua Replay 对拍最终视图。  
3. 故意绕过绑定直接改 C++ 成员 → 文档标明未定义行为。

---

## 6. 若走方案 B：落地轮廓（高成本）

### 6.1 模块拆分

```text
property_runtime.lua     -- Replay + table 形状
property_record.lua      -- Record：源表 + meta + 元表门面（赋值 / 容器代理 / 防删）
*_meta.lua               -- 属性元数据（fields / wire_kind / flags）
*_record.lua             -- 薄绑定：FLAGS + Record.bind(Meta, opts)
```

### 6.1.1 源表 + Meta + 元表门面（推荐书写）

```text
源表 obj          = 纯 Lua table（权威内存形态，new_default / 自备）
meta (*_meta)     = 字段形状、wire_kind、flags、INDEX
Record 门面       = 元表：__newindex 按 wire_kind 分流；容器读出代理；防删 / 防野字段
```

```lua
local Meta = require("Player_meta")
local data = Meta.new_default()                 -- 源表
local Record = require("property_record")
local rec = Record.open(require("Player_meta"), data) -- 或 Record.bind(Meta)

rec.hp = 80                                     -- 标量赋值 → commit(set)
rec.tags = { "warrior" }                        -- 整表替换
rec.tags:push("vip")                            -- 容器代理
rec.attrs.atk = 100                             -- dict 子键
rec.inventory:insert({ id = 1001, count = 1 })
rec.inventory[1001].count = 5                   -- ItemProxy → item_change
rec.equipment[0].enhance = 3
rec.login_history[1].logout_ts = 400
-- rec.hp = nil                                 -- error：不能删 schema 字段（用 rec:clear("hp")）
-- rec.unknown = 1                              -- error：非 schema 字段
assert(rec:data() == data)                      -- 同一张源表
local batch = rec:drain()
```

| wire_kind | 赋值 `__newindex` | 读出 `__index` |
|-----------|-------------------|----------------|
| number / string / bool | `commit(set)` + 类型校验 | 源表标量值 |
| array / vector / list | 整表 `seq_set` | 容器代理（push / item_change / …） |
| map / dict | 整表 `map_set` | 代理；子键 `attrs.atk = 100` |
| bag / slots / vec | **拒绝**整表赋值 | 代理；`[locator]` → **ItemProxy**（字段赋值入队） |

Replay 仍只吃 `(源表, meta)`：`Meta.apply_batch(data, batch)`，不必经 Record。业务约定：**只经门面写**，勿直接改裸源表。

接口覆盖测例：`examples/rpg_player/lua_record_test.lua` 中 **API matrix**（scalar / array / vector / map / bag / slots / vec 正路径 + 防删 / 防野字段 / 类型校验 / 禁止整表赋 bag·slots·vec），以及 S5/S6 场景与 C++ mutate 对拍。

### 6.2 必须与 C++ 对齐的语义清单

- flag → 是否入队（`need_flags` / `include_by`）  
- bag / slots / vec 的 `item_change` 载荷布局（slots 首参是 **格子号**）  
- 未 `resize` 的 slots insert 是否静默不同步  
- `encode_with_array` 下 add 的成对编码  
- record_offset merge 规则（若要与 C++ 队列 offset 逐 bit 一致）

### 6.3 验收（比 A 更严）

```text
同一业务脚本（或生成的操作 DSL）
    → C++ Record 队列 JSON
    → Lua Record 队列 JSON
必须 deep_equal
再 → 各自 Replay → sync_view deep_equal
```

---

## 7. 明确不做什么（本期梳理结论）

1. **不把客户端纯 Lua Replay 包升级成权威 Record**（无热更 + 防作弊：写应在服）。  
2. **不在未立项 B 的情况下**，在 `property_runtime.lua` 里偷偷加半套入队逻辑。  
3. **P5（C++ PB）与 Record-in-Lua 正交**：线格式不影响「谁权威」；A/B/C 都可继续吃 JSON mutate。

---

## 8. 建议的下一步（仅排序，不开工）

| 顺序 | 动作 |
|------|------|
| 1 | 产品确认场景：主服脚本写？还是独立 Lua 权威？ |
| 2 | 主服 → 详设 **方案 A**（绑定库选型 + Meta 生成绑定面） |
| 3 | 仅工具 → 先做 **方案 C** 标量糖，看是否够用 |
| 4 | 真要独立 Lua 权威 → 单独立项 **方案 B** + 队列对拍 CI |

---

## 9. 一句话

- **今天：** Record 只在 C++；Lua 只 Replay —— 这是有意设计，不是遗漏。  
- **要让 Lua「能写」：** 默认用 **绑定调 C++（方案 A）**；只有「无 C++ 权威进程」才值得上 **纯 Lua Record（方案 B）**；工具可用 **组 msg + apply（方案 C）** 过渡。
