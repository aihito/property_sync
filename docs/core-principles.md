# property_sync 核心原理

> **给谁看：** 需要接入、维护或评估本库的服务端 / 客户端 / 工具链工程师。  
> **读完应能回答：** 它解决什么、不解决什么；一条属性变更如何从业务写操作变成对端镜像；为什么要用 Proxy / Meta / Flag；bag、slots、vec 怎么选。

**一句话：** 业务只能通过 **Proxy** 改属性；Proxy 改本地内存的同时，向队列写入一条「改了谁、怎么改、新值是什么」的账本；对端按同样规则 **Replay**，两边状态对齐。权威在服务端（或 Record 侧），镜像侧只回放。

建议路径：本文 → 跑 [`examples/rpg_player/`](../examples/rpg_player/) 对照日志 → [game-example.md](./game-example.md) → 需要时再下钻源码与演进文档。

---

## 1. 定位：同步「账本」，不是网络库

### 1.1 游戏里真实发生的事

以玩家 Alice 为例（服务端权威，周围观察者客户端各有一份镜像）：

| 画面 | 服务端 | 观察者需要知道什么 |
|------|--------|--------------------|
| 改昵称 | `nickname = "Alice"` | 新名字，刷头顶字 |
| 扣血 | `hp = 80` | 新血量，刷血条 |
| 药水 1→5 | 背包 id=1001 的 `count` | **只这一件的数量**，不必整包重发 |
| 改金币 | `gold = 99999` | **通常什么都不用**（别人不该看） |

属性同步通常分两类（类似引擎里的 Replication）：

1. **进视野全量**：A 进入 B 视野时，把 A 对 B 可见的数据整包发给 B（本库提供 `encode_with_flag` 打包）。  
2. **视野内增量**：之后只推「变了的那几条」——**这是本库的核心能力**。

### 1.2 本库管什么 / 不管什么

| 管 | 不管（交给宿主） |
|----|------------------|
| Record：改属性时记账 | TCP/UDP、可靠序、压缩 |
| Replay：按账本回放 | AOI / 谁在视野里 |
| Flag 过滤可见性 | 登录协议、会话管理 |
| 全量快照 encode / 增量 mutate 队列 | 热更、独立于版本的脚本替换 |

```text
业务改属性
   │
   ▼
┌────────────┐   mutate_msg（账本）   ┌────────────┐
│ Server 对象 │ ────────────────────► │ Client 镜像 │
│  (Record)  │    路径+命令+数据      │  (Replay)  │
└────────────┘                       └────────────┘
       │                                    ▲
       │ 宿主：帧末 dump → 按 AOI 发送       │ 宿主：收包后 decode → replay
       └────────────────────────────────────┘
```

**边界铁律：** 本库保证「同一条 mutate 序列 + 同一份初始快照 ⇒ 镜像一致」。网络丢包、乱序、谁该收，由上层负责。

---

## 2. 核心模型：Record / Replay / mutate_msg

### 2.1 一条变更回答四个问题

队列元素是 `mutate_msg`（见 `include/property_basic.h`）：

| 问题 | 字段 | 含义 |
|------|------|------|
| 改了哪？ | `offset` | 属性路径（字段索引压进 `uint64`） |
| 怎么改？ | `cmd` | `set` / `push` / `item_change` / … |
| 谁能看？ | `flag` | `sync_clients`、`save_db` 等位掩码 |
| 新数据？ | `data` | JSON 载荷（与 any_container 编解码一致） |

示例真实输出（`rpg_player_example`）：

```text
[sync] set flag=10 data="Alice"           ← 改昵称
[sync] set flag=10 data=80                ← 改血量
[sync] add flag=11 data=[[0,1001],[1,1]]  ← 背包插入药水
[sync] item_change flag=10 data=[0,2,1,5] ← 只改该道具 count→5
```

最后一条的直觉：

- `cmd = item_change`：不是整包替换背包，而是「容器内某一件的某字段变了」  
- `data` 形如 `[定位, 字段路径, 子命令, 新值]`（bag 用稠密下标；slots 用**格子号**）  
- 观察者 Replay 后只动对应字段，其它不动  

### 2.2 常用命令

| cmd | 白话 |
|-----|------|
| `set` / `clear` | 整值赋值 / 清空 |
| `add` / `erase` | map、bag 等增删 |
| `push` / `pop` | 列表尾部增删 |
| `item_change` | 容器/数组里某一项的内部修改（细粒度同步的关键） |
| `slot_resize` / `slot_swap` / `slot_move` | 装备栏格子操作 |
| `update_fields` | 批量更新字段 |

完整枚举见文末附录。

### 2.3 端到端最小用法

```cpp
// Record 侧
std::vector<property_flags> need_flags{
    property_flags{rpg_property_flags::sync_clients}};
top_msg_queue queue(need_flags, /*ignore_default=*/true, /*with_array=*/true);

Player server;
prop_record_proxy<Player> sp(server, queue, {},
    property_flags{rpg_property_flags::mask_all});

sp.hp().set(80);
sp.inventory().get(1001)->count().set(5);

// 宿主：dump queue → 发送

// Replay 侧
Player client;
prop_replay_proxy<Player> cp(client);
while (!queue.empty()) {
    auto msg = queue.front();
    queue.pop_front();
    cp.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data);
}
```

---

## 3. 为什么必须有 Proxy

业务若写 `player.m_hp = 80`，编译器无法自动通知同步系统。可选路线：

| 做法 | 问题 |
|------|------|
| 每字段手写 setter + 同步逻辑 | 能用；容器一多代码膨胀 |
| 宏 / 平台专用属性语法 | 可移植性差，复杂逻辑难写 |
| **Proxy（本库）** | `hp().set(80)`：改内存 + 入队；容器逻辑集中在模板特化 |

**铁律：绕过 Proxy 直接改 `m_hp` / 容器内部 = 本地变了、对端永远不知道。**

- 标量与 `vector` / `map` / `array` → `property_stl.h`  
- 自定义 `Meta(property)` 类 → Meta 生成的 `*.proxy.inch`  

---

## 4. 路径（offset）：如何定位深层字段

嵌套很常见：

```text
Player.inventory[某件].count
Player.equipment[某格].enhance
Player.login_history[下标].logout_ts
```

账本不能只写 `"count"`，必须带完整地址。本库用一个 **`uint64_t` 路径**（约 8 层、每层字段索引 &lt; 256）：

| 阶段 | 类型 | 作用 |
|------|------|------|
| 写入队列 | `property_record_offset` | merge 子字段（编码约定含「+1 防 0」） |
| 回放 | `property_replay_offset` | 由 record 转换而来，再逐层 `split` |

业务侧只需记住：**深层修改靠 offset 拼出来，不是字符串 `"a.b.c"`。**  
实现细节见 `property_basic.h`；Lua 侧对拍用同一套语义（`record_offset` → path）。

---

## 5. 三层队列：为何不是「业务自己拼完整路径」

最终待发送消息进 **`top_msg_queue`**。深层写入时用「信封」自动 merge 父路径：

| 层级 | 类比 | 作用 |
|------|------|------|
| `top_msg_queue` | 邮筒 | 存放 `mutate_msg` |
| `aggregation_msg_queue` | 嵌套结构体信封 | 子字段写入时 merge 父路径 |
| `item_msg_queue` | 背包单件信封 | 把「某件内部字段变更」打成一条 `item_change` |

```text
业务：inv.get(1001)->count().set(5)
        │
        ▼
  item 信封：这件 + count + set + 5
        │
        ▼
  顶层邮筒：一条 item_change
        │
        ▼
  帧末 dump → 宿主按 AOI 发出（本库之外）
```

创建队列时传入的 `need_flags` 决定邮筒收哪些信（例如只收 `sync_clients`）。

---

## 6. 值类型选型（决策树）

```text
要同步的数据是什么？
│
├─ 单个数字 / 字符串 / bool          → 标量 Proxy（set/clear）
├─ 简单列表 / 字典（元素也简单）      → std::vector / unordered_map / array
└─ 元素是复杂结构，且常改内部字段
      ├─ 按业务 id 查找               → property_bag     （道具、Buff）
      ├─ 有固定格子、要换位           → property_slots   （装备栏）
      └─ 顺序本身就是意义             → property_vec     （登录记录）
```

| 类型 | 改内部字段时同步什么 | 例子 |
|------|----------------------|------|
| 标量 / 简单 STL | 该字段的增量命令 | HP；tags push；attrs 插 atk |
| **bag** | 通常一条 `item_change`，非整件重传 | 药水 `count` 1→5 |
| **slots** | 先 `resize` 再 insert/swap/move | 强化、两格对换 |
| **vec** | 按下标 push/insert；改字段同样 `item_change` | 补写某次登录的 `logout_ts` |

基类关系：

```text
property_vec_item
      ↑
property_bag_item   （多了 id）
      ↑
property_slot_item  （再多了 slot）
```

选型直觉：**bag = 按 id 的包；slots = 有格的栏；vec = 有序记录列表。**  
场景演示见 [game-example.md](./game-example.md)。

---

## 7. Flag：同一修改，不同通道看到不同东西

字段注解带上可见性（如 `sync_clients`、`save_db`）。队列创建时声明订阅哪些 flag：

| 操作 | 服务端内存 | sync_clients 队列 |
|------|------------|-------------------|
| `hp().set(80)` | 变 | 有 `set` |
| `gold().set(99999)`（仅 save_db） | 变 | **空** |
| `item.name().set(...)`（仅 save_db） | 变 | **空** |
| `item.count().set(5)`（sync_clients） | 变 | 有 `item_change` |

全量快照同样按 flag 过滤：`encode_with_flag(sync_clients)` 不会带上别人不该看的金币等。

属性「四要素」：

| 要素 | 含义 |
|------|------|
| 名字 | `m_hp` → 生成 `hp()` |
| 值类型 | 标量 / STL / bag / slots / vec |
| 同步可见性 | 谁能看见（sync_*） |
| 生命周期/存库 | 是否进库（save_db）等 |

后两者落在 **同一套 flag 位掩码** 上（`include_by`：订阅方 flag 必须被数据 flag 完全覆盖）。

---

## 8. Meta 代码生成：唯一真相源

Record/Replay、字段索引、encode/decode、Proxy 特化手写极易漏。流程：

```text
头文件 Meta(property) 标注
        │
        ▼
generate_property_sync（libclang + mustache）
        │
        ├─ *.generated.inch / *.proxy.inch / *.incpp   ← C++ Record/Replay
        ├─ generated/schema/*.schema.json             ← 兼容合同
        ├─ generated/proto/*.proto                    ← Snapshot / Mutate IDL
        └─ generated/lua/*_sync.lua + property_runtime.lua  ← 纯 Lua Replay
```

硬性约定：

| 规则 | 说明 |
|------|------|
| 成员必须以 `m_` 开头 | 对外 API 去前缀：`m_hp` → `hp()` |
| 类要有 `Meta(property)` | 才会进入生成 |
| flag 名与 `flag_class` 静态成员一致 | 如 `sync_clients` |
| 生成文件名 = 类名 | `Player.proxy.inch` |

解析时必须给对 Clang **`-resource-dir`**，且 include 能找到 `any_container` 等；否则基类解析失败，背包 item 会生成错误 Proxy（示例 CMake 已处理）。

生成器内部已拆成 **字段分类（classify）→ ClassModel → 多产物 mustache**，扩展新 wire 类型优先改 classifier，而不是复制粘贴多处 if/else。见 `meta/generate_property_sync.cpp`。

命令与依赖：[build-and-test.md](./build-and-test.md)。

---

## 9. 跨语言镜像（当前能力，高层）

目标：客户端可用 **纯 Lua** 回放与 C++ 同版本的 mutate / snapshot，**不支持**单独热更 sync 脚本。

| 产物 | 作用 |
|------|------|
| schema.json + `diff_schema.py` | 拦破坏性字段变更 |
| `*_sync.lua` + `property_runtime.lua` | 纯 Lua `apply_mutate` / `load_snapshot` |
| `.proto` | Snapshot / Mutate IDL（`protoc` 可编译；C++ PB 编解码属后续阶段） |

验收入口：

```bash
cmake --build build --target rpg_player_lua_replay   # batch + snapshot + mixed
cmake --build build --target rpg_player_proto_check  # protoc
./build/test/property_test                           # C++ Record/Replay
```

细则：[compatibility.md](./compatibility.md)、[lua-sync.md](./lua-sync.md)、[protobuf.md](./protobuf.md)、[evolution-plan.md](./evolution-plan.md)。

---

## 10. 设计约束（实现上限）

| 约束 | 原因 |
|------|------|
| 单类字段 ≤ 255 | 路径压进 `uint64_t` |
| 嵌套深度 ≤ 8 | 同上 |
| 必须走 Proxy | 否则无法 Record |
| 不管网络 | dump 之后由宿主广播 |
| 无热更 sync 脚本 | schema_version 与整包锁定 |

Proxy 相对「每字段生成一整套 setter」的好处：容器逻辑集中在模板，少膨胀、好维护。

---

## 11. 源码导航

| 路径 | 内容 |
|------|------|
| `include/property.h` | 总入口 |
| `include/property_basic.h` | offset、cmd、flag、`mutate_msg` |
| `include/property_queue.h` | top / aggregation / item 队列 |
| `include/property_stl.h` | 标量与 STL Proxy |
| `include/property_bag.h` / `property_slots.h` / `property_vec.h` | 三种复杂容器 |
| `meta/` | 代码生成与 lua_runtime |
| `examples/rpg_player/` | 分场景演示（对照本文最有效） |
| `test/` | 更全的 C++ Record/Replay 用例 |
| `docs/` | 兼容 / Proto / Lua / 演进方案 |

---

## 附录 A：完整 `property_cmd`

| 命令 | 用途 |
|------|------|
| `clear` / `set` | 清空 / 全量赋值 |
| `add` / `erase` | map、bag 等增删 |
| `push` / `pop` / `pop_erase` | 列表尾部操作 |
| `item_change` | 容器内元素字段级修改 |
| `slot_swap` / `slot_resize` / `slot_move` | 槽位操作 |
| `update_fields` | 批量更新字段 |

## 附录 B：最小心智模型（可撕下带走）

```text
1. 只通过 Proxy 写属性
2. 每条变更 = offset + cmd + flag + data
3. 细粒度靠 item_change，不要整包刷
4. Flag 决定「谁能看见」；encode / 队列共用同一套
5. Meta 生成 C++/schema/proto/lua；版本整包走，不热更 sync
6. 本库是账本；网络与 AOI 在宿主
```
