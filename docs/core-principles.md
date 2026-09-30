# property_sync 核心原理

本文整理本库的设计目标与核心机制，对应源码主要在 `include/` 与 `meta/`。

## 1. 要解决什么问题

游戏角色属性（存档）需要在服务端修改后，把**可见变更**推给相关客户端，类似 Unreal Replication：

| 场景 | 行为 |
|------|------|
| A 进入 B 的视野 | 把 A 对 B 可见的数据**全量**发给 B |
| 视野内 A 的属性变化 | 只推送**增量变更**（本库重点） |

本库抽象掉网络与 AOI，只做两件事：

1. **Record**：服务端改属性时，自动把变更写入消息队列  
2. **Replay**：客户端（或镜像对象）按消息回放，与服务端收敛一致  

```
┌─────────────┐   mutate_msg 队列    ┌─────────────┐
│  Server 对象 │ ─────────────────► │ Client 镜像 │
│ (record)    │   offset+cmd+data   │ (replay)    │
└─────────────┘                     └─────────────┘
```

## 2. 属性四要素

每个字段定义包含：

| 要素 | 含义 | 本库体现 |
|------|------|----------|
| 名字 | 字段名 | 成员变量 + meta 生成 getter/proxy |
| 值类型 | 基础值 / 容器 / 结构体 / 背包 | `int`、`vector`、`map`、`property_bag` 等 |
| 同步类型 | 谁能看见 | `property_flags`（如 `sync_clients`） |
| 生命周期 | 是否存库 | `property_flags`（如 `save_db`） |

同步与存库通过 **flag 位掩码** 过滤：队列订阅需要的 flag，只有匹配的变更才会入队。

常见 flag（见 `test/prop_flags.h`）：

- `save_db`：需要持久化  
- `sync_self` / `sync_other`：同步给自己 / 他人客户端  
- `sync_ghost`：同步给 ghost 镜像  
- `sync_clients = sync_self | sync_other`

## 3. 整体架构

```
业务定义 Meta(property) 类
        │
        ▼
   meta 代码生成
   (.generated.inch / .proxy.inch)
        │
        ▼
┌───────────────────────────────────────────┐
│ prop_record_proxy<T>  ──► 改值 + 写队列   │
│ prop_replay_proxy<T>  ──► 读消息 + 回放   │
│ msg_queue (top/aggregation/item)          │
│ property_record_offset / replay_offset    │
└───────────────────────────────────────────┘
```

### 3.1 Proxy（代理）而非手写 setter

直接改成员无法拦截。本库用 **proxy** 封装读写：

```cpp
// 服务端：通过 record proxy 修改
auto player_proxy = prop_record_proxy<Player>(server_player, queue, offset, flags);
player_proxy.hp().set(80);           // 改本地值 + 入队 set 消息
player_proxy.inventory().insert(...); // 容器增量同步

// 客户端：通过 replay proxy 回放
prop_replay_proxy<Player> client_proxy(client_player);
client_proxy.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data);
```

基础类型、STL 容器的 proxy 在 `property_stl.h`；自定义属性类的 proxy 由 meta 生成。

### 3.2 变更命令 `property_cmd`

| 命令 | 用途 |
|------|------|
| `set` / `clear` | 全量赋值 / 清空 |
| `add` / `erase` | map / bag 增删 |
| `push` / `pop` | vector 尾部增删 |
| `item_change` | 容器内元素的字段级修改 |
| `slot_swap` / `slot_resize` / `slot_move` | 槽位背包操作 |
| `update_fields` | 批量更新字段 |

一条消息：

```cpp
struct mutate_msg {
    property_record_offset offset; // 属性路径
    property_cmd cmd;              // 操作类型
    property_flags flag;           // 本条变更的可见性
    json data;                     // 载荷
};
```

### 3.3 多级路径编码（offset）

嵌套访问如 `player.base.level` 需要路径。本库用 **单个 `uint64_t`** 编码最多 8 层、每层最多 255 个字段（0 号保留）：

- `property_record_offset`：写入时用（每层 index+1，避免 0 歧义）  
- `property_replay_offset`：回放时用（字节序转换后的路径）  

```
merge 示例：parent_offset.merge(child_index)
→ 父路径左移 8 位 | (子 index + 1)
```

这样路径紧凑，也利于 msgpack/protobuf 等小整数优化。

### 3.4 消息队列分层

| 类型 | 职责 |
|------|------|
| `top_msg_queue` | 根对象队列，真正存放 `mutate_msg` |
| `aggregation_msg_queue` | 嵌套结构体：把子 offset merge 到父路径 |
| `item_msg_queue` | 背包 item：把内部字段变更包装成 `item_change` |

```
改 bag[id=3].count
  → item_msg_queue 记录 (item_idx=?, field_offset, set, value)
  → 父队列收到 item_change + encode_multi(item_idx, offset, cmd, data)
```

## 4. 值类型与同步粒度

### 4.1 基础值

`int` / `float` / `bool` / `string` / `json`：`set` / `clear`。

### 4.2 STL 容器

- `vector<T>`：`set` / `clear` / `push` / `pop` / 按索引改删  
- `unordered_map<K,V>`（K 为 int 或 string）：`insert` / `erase` / `clear`  
- `array<T,N>`：`set` / 按元素改  

容器只同步**变更部分**，不做无意义的全量推送。

### 4.3 结构体（Meta property 类）

一组相关字段的集合（如 `base_info`）。子字段变更通过 `aggregation_msg_queue` 拼出完整路径。

### 4.4 三种「背包」抽象

| 类型 | 模型 | 典型用途 |
|------|------|----------|
| `property_bag` | `map<id, item>` | 道具/任务/buff（按 id 索引） |
| `property_slots` | 固定槽位 + id | 装备栏（有格子概念） |
| `property_vec` | 有序 item 列表 | 登录记录、剧情步骤等 |

item 可再嵌套字段甚至子背包；字段级修改走 `item_msg_queue`，避免每次改一个数字就整包同步 item。

## 5. Record / Replay 闭环

```
服务端 tick 内业务逻辑
        │
        ▼
prop_record_proxy 修改属性
        │
        ▼
top_msg_queue 积累 mutate_msg
        │
        ▼
帧末 dump / 网络发送（本库之外）
        │
        ▼
客户端 prop_replay_proxy.replay(...)
        │
        ▼
本地对象与服务器一致
```

测试里的最小闭环（`test/main.cpp`）：

```cpp
server_proxy.a().set(1);
auto msg = queue.front(); queue.pop_front();
client_proxy.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data);
assert(server == client);
```

## 6. Meta 代码生成与命名约定

手写每个字段的 encode / decode / replay / proxy 成本高。流程：

1. 用 `Meta(property(...))` 标注类与字段  
2. `meta/generate_property_sync`（基于 Clang AST）解析标注  
3. Mustache 模板生成：  
   - `*.generated.inch` / `*.generated.incpp`：成员索引、编解码、replay 分发  
   - `*.proxy.inch`：`prop_record_proxy` 特化  

### 命名约定（生成器硬性要求）

| 规则 | 说明 |
|------|------|
| 成员必须以 `m_` 开头 | 生成器只收集 `m_` 前缀字段；对外 API 会去掉此前缀（`m_hp` → `hp()`） |
| 类标 `Meta(property)` | 才会进入属性代码生成 |
| flag 名与 `flag_class` 静态成员一致 | 如 `sync_clients`、`save_db` 对应 `test_property_flags::sync_clients` |
| 生成文件名 = 类名 | `simple_bag_item.generated.inch`、`Player.proxy.inch` |

标注示例：

```cpp
class Meta(property) Player {
    Meta(property(sync_clients)) int m_hp;           // proxy: hp()
    Meta(property(save_db, sync_clients)) property_bag<Item> m_bag; // proxy: bag()
};
```

生成时需给 libclang 正确的 `-resource-dir`（否则解析失败，`has_base_class` 丢失，背包 item 的 proxy 签名会错）。
## 7. 设计约束与取舍

| 约束 | 原因 |
|------|------|
| 单类字段 ≤ 255（`uint8_t` 索引） | 路径压缩进 `uint64_t` |
| 嵌套深度 ≤ 8 | 同上 |
| 业务改属性必须走 proxy | 否则无法 record |
| 本库不管网络发送 | 队列 dump 后由宿主框架广播 |

**Proxy 方案相对「每字段生成一套 setter」的优势**：容器操作逻辑集中在模板特化里，避免按字段复制膨胀，编译体积与维护性更好。

## 8. 源码导航

| 路径 | 内容 |
|------|------|
| `include/property_basic.h` | offset、cmd、flags、消息结构 |
| `include/property_queue.h` | top / aggregation / item 队列 |
| `include/property_stl.h` | 基础类型与 STL proxy |
| `include/property_bag.h` | 按 id 的背包 |
| `include/property_slots.h` | 槽位背包 |
| `include/property_vec.h` | 有序 item 列表 |
| `meta/` | 代码生成器与 Mustache 模板 |
| `test/` | 完整 Record/Replay 单元演示 |
| `examples/rpg_player/` | 游戏向使用示例 |

## 9. 下一步

- 结合场景阅读：[游戏使用示例](./game-example.md)  
- 对照跑通：`test/main.cpp` 中的 record → replay 断言  
- 自定义属性：照 `examples/rpg_player` 写 Meta 类，再跑 meta 生成  
