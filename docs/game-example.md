# 游戏使用示例：RPG 玩家属性同步

完整可编译代码：[`examples/rpg_player/`](../examples/rpg_player/)。运行后按章节打印每条 `mutate_msg`，便于对照。

## 场景总览

| 章节 | 类型 | 演示点 |
|------|------|--------|
| 1 | 基础值 | `set` / `clear` 昵称、HP、等级 |
| 2 | `array` | 坐标整表赋值 + 改单个分量 |
| 3 | `vector` | 标签 `set` / `push` / `pop` / `erase_multi` |
| 4 | `map` | 属性表 insert / 覆盖 / erase |
| 5 | **`property_bag`** | 道具按 id：插入、改 count、erase、`get_insert` |
| 6 | **`property_bag`** | Buff 叠层、改到期时间 |
| 7 | **`property_slots`** | 装备栏 resize、换位、挪格、按 id/格删除 |
| 8 | **`property_vec`** | 登录记录有序 push、改字段、中间 insert |
| 9 | flag | `gold` / item.`name` / record.`ip` 仅 `save_db` |

```
Server Alice  ──mutate_msg──►  Client 观察者镜像
  nickname / hp / pos / tags / attrs
  inventory(bag)  buffs(bag)
  equipment(slots)  login_history(vec)
```

## 属性建模（与代码一致）

```cpp
// bag item
class Item : public property_bag_item<int> {
    Meta(property(sync_clients)) int m_count;
    Meta(property(save_db)) std::string m_name;   // 不同步给观察者
};
class Buff : public property_bag_item<int> { ... };

// slots item
class EquipItem : public property_slot_item<int> {
    Meta(property(sync_clients)) int m_enhance;
    Meta(property(sync_clients)) std::string m_name;
};

// vec item（无业务主键，靠下标访问）
class LoginRecord : public property_vec_item {
    Meta(property(sync_clients)) float m_login_ts;
    Meta(property(sync_clients)) float m_logout_ts;
    Meta(property(save_db)) std::string m_ip;
};

class Player {
    // 基础值 + STL
    Meta(property(sync_clients)) std::string m_nickname;
    Meta(property(sync_clients)) int m_hp, m_level;
    Meta(property(save_db)) int m_gold;
    Meta(property(sync_clients)) std::array<float,3> m_pos;
    Meta(property(sync_clients)) std::vector<std::string> m_tags;
    Meta(property(sync_clients)) std::unordered_map<std::string,int> m_attrs;
    // 三种背包
    Meta(property(save_db, sync_clients)) property_bag<Item> m_inventory;
    Meta(property(save_db, sync_clients)) property_bag<Buff> m_buffs;
    Meta(property(save_db, sync_clients)) property_slots<EquipItem> m_equipment;
    Meta(property(save_db, sync_clients)) property_vec<LoginRecord> m_login_history;
};
```

## 三种背包怎么记

| | bag | slots | vec |
|--|-----|-------|-----|
| 模型 | `id → item` | 固定格子 + id | 有序下标 |
| 示例 | 药水、Buff | 装备栏 | 登录记录 |
| 特色操作 | `get(id)` / `erase(id)` | `resize` / `swap_slot` / `move_slot` | `push` / `insert(idx)` / `pop` |
| 字段增量 | ✅ `item_change` | ✅ | ✅ |

注意：`property_slots` **先 `resize` 再 `insert`**，未扩容时 insert 不会进同步队列（示例第 7 节有意演示）。

## 怎么跑

```bash
cmake --build build --target rpg_player_example -j
./build/examples/rpg_player/rpg_player_example
```

成功输出末尾：`[PASS] 全部场景通过...`

## 对照阅读

| 想理解… | 看哪里 |
|---------|--------|
| 逐步命令与 data | `examples/rpg_player/main.cpp` 各 `section` |
| 原理 | [core-principles.md](./core-principles.md) |
| 编译命令 | [build-and-test.md](./build-and-test.md) |
