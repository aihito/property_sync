# property_sync 核心原理

> **一句话：** 业务不能直接改字段，必须通过 Proxy；Proxy 一边改本地内存，一边往队列记一条「改了谁、怎么改、新值是什么」；对端按同样规则 Replay，两边数据对齐。

建议阅读顺序：本文 → 跑 [`rpg_player_example`](../examples/rpg_player/) 对照日志 → [game-example.md](./game-example.md) → 需要时再看源码导航。

---

## 1. 游戏里发生了什么

以玩家 Alice 为例（服务端权威，周围玩家客户端各有一份镜像）：

| 画面 | 服务端做了什么 | 观察者客户端需要知道什么 |
|------|----------------|--------------------------|
| 改昵称 | `nickname = "Alice"` | 只要新名字，刷新头顶字 |
| 扣血 | `hp = 80` | 只要新血量，刷血条 |
| 药水数量 1→5 | 背包里 id=1001 的 `count` | 只要这一件的数量，不必整包重发 |
| 改金币 | `gold = 99999` | **通常什么都不用**（别人不该看到） |

属性同步类似 Unreal Replication，分两类：

1. **进视野全量**：A 进入 B 视野时，把 A 对 B 可见的数据整包发给 B（encode 快照，本库能打包，但网络/AOI 不在本库）。  
2. **视野内增量**：之后只推「变了的那几条」——**这是本库的核心**。

本库刻意不管：TCP/UDP、AOI、谁在不在视野。它只提供：

- **Record**：改属性时自动记账  
- **Replay**：对端按账本回放  

```
业务改属性
   │
   ▼
┌────────────┐  mutate_msg（账本）  ┌────────────┐
│ Server 对象 │ ─────────────────► │ Client 镜像 │
│  (Record)  │   路径+命令+数据     │  (Replay)  │
└────────────┘                     └────────────┘
```

---

## 2. 一条变更长什么样

队列里每一条都是 `mutate_msg`，可以想成回答四个问题：

| 问题 | 字段 | 含义 |
|------|------|------|
| 改了哪？ | `offset` | 属性路径（如 hp、或背包某件的 count） |
| 怎么改？ | `cmd` | set / push / item_change / … |
| 谁能看？ | `flag` | sync_clients、save_db 等 |
| 新数据？ | `data` | JSON 载荷 |

示例程序真实输出（节选）：

```text
[sync] set flag=10 data="Alice"           ← 改昵称
[sync] set flag=10 data=80                ← 改血量
[sync] add flag=11 data=[[0,1001],[1,1]]  ← 背包插入药水
[sync] item_change flag=10 data=[0,2,1,5] ← 只改该道具的 count→5
```

翻译最后一条 `item_change`（示意）：

- `cmd = item_change`：不是整包替换背包，而是「容器内某一件的某字段变了」  
- `data ≈ [item 定位, 字段路径, 子命令 set, 新值 5]`  
- 观察者 Replay 后，本地 Alice 镜像里药水数量变成 5，其它字段不动  

常用命令（够用即可，完整枚举见附录）：

| cmd | 白话 |
|-----|------|
| `set` / `clear` | 整值赋值 / 清空 |
| `add` / `erase` | map、bag 增删 |
| `push` / `pop` | 列表尾部增删 |
| `item_change` | 容器/数组里某一项的内部修改 |
| `slot_resize` / `slot_swap` / `slot_move` | 装备栏格子操作 |

---

## 3. 为什么必须有 Proxy

如果业务写 `player.m_hp = 80`，编译器无法自动「通知同步系统」。必须拦截写入。

常见三条路：

| 做法 | 问题 |
|------|------|
| 每个字段手写 setter，里面塞同步逻辑 | 能用，容器一多代码膨胀 |
| 宏 / MSVC `__declspec(property)` | 平台不一致，复杂逻辑难写 |
| **Proxy（本库）** | `hp().set(80)`：改内存 + 入队；容器操作集中在模板特化里 |

```cpp
prop_record_proxy<Player> sp(server, sync_queue, {}, flags_all);

sp.hp().set(80);                    // Record
sp.inventory().get(1001)->count().set(5);

// 对端
prop_replay_proxy<Player> cp(client);
cp.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data);  // Replay
```

**铁律：绕过 Proxy 直接改 `m_hp` / 容器内部 = 本地变了、对端永远不知道。**

- 基础类型与 `vector` / `map` / `array` 的 Proxy → `property_stl.h`  
- 自定义 `Meta(property)` 类的 Proxy → meta 生成的 `*.proxy.inch`  

---

## 4. 路径：怎么定位到「深层字段」

嵌套很常见，例如：

```text
Player.inventory[某件].count
Player.equipment[某格].enhance
Player.login_history[下标].logout_ts
```

账本不能只写 `"count"`，必须带完整地址。本库用一个 **`uint64_t` 路径**（最多约 8 层、每层字段索引 < 256）：

- 写队列时用 `property_record_offset`（merge 子字段时带编码约定）  
- 回放时转成 `property_replay_offset`，再一层层拆开找到目标  

直觉：

```text
改 inventory 里某件的 count
  = 路径( inventory ) +「第几件」+ 路径( count ) + 命令 set + 值 5
```

细节（+1 防 0、record/replay 字节序）是实现优化，阅读代码时再看 `property_basic.h` 即可；业务侧只要知道：**深层修改靠 offset 拼出来，不是靠字符串 `"a.b.c"`。**

---

## 5. 队列为什么分三层（信封比喻）

最终要发出去的信，都进 **顶层队列** `top_msg_queue`。

写深层字段时，不能让每个业务自己拼完整地址，所以有两层「信封」：

| 层级 | 类比 | 作用 |
|------|------|------|
| `top_msg_queue` | 邮筒 | 真正存放待发送的 `mutate_msg` |
| `aggregation_msg_queue` | 嵌套结构体的信封 | 子字段写入时自动 merge 父路径 |
| `item_msg_queue` | 背包单件的信封 | 把「某件内部字段变更」打成一条 `item_change` |

```text
业务：inv.get(1001)->count().set(5)
        │
        ▼
  item 信封：记下「这件 + count + set + 5」
        │
        ▼
  顶层邮筒：一条 item_change 消息
        │
        ▼
  帧末 dump → 宿主框架按 AOI 发出去（本库之外）
```

订阅方用 `need_flags` 决定邮筒收哪些信（例如只收 `sync_clients`）。

---

## 6. 值类型怎么选（决策树）

```text
要同步的数据是什么？
│
├─ 单个数字 / 字符串 / bool     → 标量 Proxy（set/clear）
├─ 简单列表 / 字典（元素也简单） → std::vector / unordered_map / array
└─ 元素本身是「复杂结构」，且常改内部字段
      ├─ 按业务 id 查找          → property_bag     （道具、Buff）
      ├─ 有固定格子、要换位      → property_slots   （装备栏）
      └─ 顺序本身就是意义        → property_vec     （登录记录、步骤）
```

| 类型 | 改内部字段时同步什么 | 例子 |
|------|----------------------|------|
| 标量 / 简单 STL | 该容器的增量命令 | HP；tags push；attrs 插入 atk |
| **bag** | 通常一条 `item_change`，不是整件重传 | 药水 `count` 1→5 |
| **slots** | 先有格子（`resize`），再 insert/swap/move | 强化等级、两格对换 |
| **vec** | 按下标 push/insert；改某条字段同样 `item_change` | 补写某次登录的 `logout_ts` |

三种背包的 item 基类关系：

```text
property_vec_item
      ↑
property_bag_item   （多了 id）
      ↑
property_slot_item  （再多了 slot）
```

选型直觉：**bag = 按 id 的包；slots = 有格的栏；vec = 有序记录列表。**  
更细的场景见 [game-example.md](./game-example.md)。

---

## 7. Flag：同一修改，不同通道看到不同东西

定义字段时带上 flag（如 `sync_clients`、`save_db`）。队列创建时声明「我订阅哪些 flag」：

- 只订阅 `sync_clients` → 观察者同步通道  
- 另开队列订阅 `save_db` → 增量存库通道（真实项目常见）

示例里的现象：

| 操作 | 服务端内存 | sync_clients 队列 |
|------|------------|-------------------|
| `hp().set(80)` | 变 | 有 `set` |
| `gold().set(99999)`（仅 save_db） | 变 | **空** |
| `item.name().set(...)`（仅 save_db） | 变 | **空** |
| `item.count().set(5)`（sync_clients） | 变 | 有 `item_change` |

全量快照也按 flag 过滤：`encode_with_flag(sync_clients)` 不会带上别人不该看的金币等字段。

属性「四要素」在这里才完整：

| 要素 | 含义 |
|------|------|
| 名字 | `m_hp` → 生成 `hp()` |
| 值类型 | 标量 / STL / bag / slots / vec |
| 同步可见性 | 谁能看见（sync_*） |
| 生命周期/存库 | 是否进库（save_db）等 |

后两者在本库里都落在 **同一套 flag 位掩码** 上。

---

## 8. Meta：省掉手写的编译期工具

Record/Replay、字段索引、encode/decode、Proxy 特化，手写极易漏。流程：

1. 类/字段标 `Meta(property(...))`  
2. `generate_property_sync` + `config.json` 用 libclang 解析  
3. 生成 `*.generated.inch` / `*.proxy.inch` 等  

命名硬性约定：

| 规则 | 说明 |
|------|------|
| 成员必须以 `m_` 开头 | 对外 API 去掉前缀：`m_hp` → `hp()` |
| 类要有 `Meta(property)` | 才会进入生成 |
| flag 名与 `flag_class` 静态成员一致 | 如 `sync_clients` |
| 生成文件名 = 类名 | `Player.proxy.inch` |

生成时必须给对 Clang **`-resource-dir`**，且 include 能找到 `any_container` 等依赖；否则基类解析失败，背包 item 会生成错误的 Proxy 签名（示例 CMake 已处理）。

细节命令见 [build-and-test.md](./build-and-test.md)。

---

## 9. 设计约束（附录）

| 约束 | 原因 |
|------|------|
| 单类字段 ≤ 255 | 路径压进 `uint64_t` |
| 嵌套深度 ≤ 8 | 同上 |
| 必须走 Proxy | 否则无法 Record |
| 不管网络 | dump 之后由宿主广播 |

Proxy 相对「每字段生成一整套 setter」的好处：容器逻辑集中在模板，少膨胀、好维护。

---

## 10. 源码导航（附录）

| 路径 | 内容 |
|------|------|
| `include/property.h` | 总入口 |
| `include/property_basic.h` | offset、cmd、flag、`mutate_msg` |
| `include/property_queue.h` | top / aggregation / item 队列 |
| `include/property_stl.h` | 标量与 STL Proxy |
| `include/property_bag.h` / `property_slots.h` / `property_vec.h` | 三种背包 |
| `meta/` | 代码生成 |
| `examples/rpg_player/` | 分场景演示（对照本文最有效） |
| `test/` | 更全的 Record/Replay 用例 |

---

## 附录：完整 `property_cmd`

| 命令 | 用途 |
|------|------|
| `clear` / `set` | 清空 / 全量赋值 |
| `add` / `erase` | map、bag 等增删 |
| `push` / `pop` / `pop_erase` | 列表尾部操作 |
| `item_change` | 容器内元素字段级修改 |
| `slot_swap` / `slot_resize` / `slot_move` | 槽位操作 |
| `update_fields` | 批量更新字段 |
