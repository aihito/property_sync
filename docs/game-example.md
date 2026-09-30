# 游戏使用示例：RPG 玩家属性同步

用一个简化的 RPG 场景说明 `property_sync` 怎么用。完整可编译代码见 [`examples/rpg_player/`](../examples/rpg_player/)。

## 场景设定

玩家 **Alice** 在场景里：

1. 改昵称（周围玩家应看到新名字）  
2. 扣血（自己与周围同步 HP）  
3. 获得道具、修改道具数量（背包增量）  
4. 获得 Buff、叠层（buff 背包字段级同步）  

服务端持有权威 `Player`，每个观察者客户端持有镜像 `Player`。本示例用内存队列模拟网络。

```
        Server Alice                    Client（观察者）镜像
        ┌──────────────┐                ┌──────────────┐
 set    │ nickname     │── mutate_msg ─►│ nickname     │
 set    │ hp           │── mutate_msg ─►│ hp           │
 insert │ inventory    │── mutate_msg ─►│ inventory    │
 item   │ buffs[id].lv │── mutate_msg ─►│ buffs[id].lv │
        └──────────────┘                └──────────────┘
```

## 属性怎么建模

```cpp
// 道具：背包里的 item，带 id
class Item : public property_bag_item<int> {
    Meta(property(sync_clients)) int m_count;      // 数量 → 同步客户端
    Meta(property(save_db))      std::string m_name; // 仅存库
};

// Buff
class Buff : public property_bag_item<int> {
    Meta(property(sync_clients)) int m_level;
    Meta(property(sync_clients)) float m_expire_ts;
};

// 玩家根属性
class Player {
    Meta(property(sync_clients)) std::string m_nickname;
    Meta(property(sync_clients)) int m_hp;
    Meta(property(save_db))      int m_gold;          // 只存库不同步给别人
    Meta(property(save_db, sync_clients)) property_bag<Item> m_inventory;
    Meta(property(save_db, sync_clients)) property_bag<Buff> m_buffs;
};
```

要点：

- `sync_clients`：变更会进「同步客户端」队列  
- `save_db`：可按 flag 做增量存库打包  
- `gold` 只标 `save_db`：别人看不到金币变更（符合常见设计）

## 服务端怎么改

```cpp
top_msg_queue sync_queue(/* need sync_clients */, true, true);
Player server;
prop_record_proxy<Player> sp(server, sync_queue, {}, flags_all);

// 1) 改昵称
sp.nickname().set("Alice_the_Brave");

// 2) 扣血
sp.hp().set(sp.hp().get() - 20);

// 3) 进背包一个药水，再改数量
json potion = {{"id", 1001}, {"count", 1}, {"name", "HP Potion"}};
sp.inventory().insert(potion);
sp.inventory().get(1001)->count().set(5);  // 只同步 count 字段

// 4) 加 Buff 并叠层
json buff = {{"id", 200}, {"level", 1}, {"expire_ts", 9999.0}};
sp.buffs().insert(buff);
sp.buffs().get(200)->level().set(2);
```

每次 proxy 调用都会：改本地内存 +（若 flag 匹配）往 `sync_queue` 塞一条 `mutate_msg`。

## 客户端怎么回放

```cpp
Player client;  // 镜像，初始可为空或由全量快照初始化
prop_replay_proxy<Player> cp(client);

while (!sync_queue.empty()) {
    auto msg = sync_queue.front();
    sync_queue.pop_front();
    // 真实项目：这里是网络收到的包
    cp.replay(msg.offset.to_replay_offset(), msg.cmd, msg.data);
}

assert(server == client);  // 可见字段收敛一致
```

## 消息长什么样（示意）

| 操作 | cmd | offset 含义 | data 示意 |
|------|-----|-------------|-----------|
| 改昵称 | `set` | `nickname` | `"Alice_the_Brave"` |
| 改 HP | `set` | `hp` | `80` |
| 插入道具 | `add` | `inventory` | 整份 item json |
| 改数量 | `item_change` | `inventory` | `(item_idx, field_offset, set, 5)` |
| Buff 叠层 | `item_change` | `buffs` | `(item_idx, level_offset, set, 2)` |

改 `gold` **不会**出现在 `sync_clients` 队列里（flag 不匹配）。

## 和真实游戏循环的关系

```
每帧:
  处理 RPC / Timer / 战斗结算
       │
       ▼
  业务只通过 prop_record_proxy 改属性   ← 本库
       │
       ▼
  帧末 dump top_msg_queue
       │
       ▼
  按 AOI / 订阅关系发给客户端           ← 宿主框架
       │
       ▼
  客户端 replay                         ← 本库
```

本库不实现 AOI 与发包；宿主把 `dump()` 出的消息按观察者过滤发送即可。全量进视野时用 `encode_with_flag(sync_clients)` 打一份快照，之后只用增量。

## 跑示例

见 [`examples/rpg_player/README.md`](../examples/rpg_player/README.md)。依赖与 `test/` 相同：先用 meta 生成 inch 文件，再编译运行。

## 对照阅读

| 想理解… | 看哪里 |
|---------|--------|
| Record/Replay 闭环 | `examples/rpg_player/main.cpp`、`test/main.cpp` |
| Bag 字段级同步 | `main.cpp` 里 inventory / buffs 段落 |
| Flag 过滤 | `prop_flags.h` + `test_flags` |
| 原理总览 | [core-principles.md](./core-principles.md) |
