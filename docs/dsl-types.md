# DSL 数据类型一览（对照 C++ 现状 × Lua）

> **目的：** 全面整理 DSL 支持哪些类型；以当前 C++ `property_*` 实现为参照，给出 **DSL ↔ C++ ↔ Lua** 对照，便于双运行时 Record/Replay。  
> **配套：** [dsl-design.md](./dsl-design.md)（语法）、[core-principles.md](./core-principles.md)（语义）。

**读表约定**

| 列 | 含义 |
|----|------|
| **C++ 现状** | 库里已有 `prop_record_proxy` / Replay 特化 |
| **DSL** | `.psync` 规范写法（线语义） |
| **Lua 落点** | Record/Replay 本地存储约定 |
| **DSL 状态** | `✅ 纳入` / `✅ 别名` / `⏳ 后期` / `❌ 不进 DSL` |

---

## 1. 总览：按能力分层

```text
① 标量 scalar
② 简单容器（元素=标量） array / list / dict
③ 复杂容器（元素=结构体 item） bag / slots / vec
④ 嵌套属性对象 object
⑤ 类种类（声明 item/entity 自身） bag_item / slot_item / vec_item / entity
```

C++ 现状来源：`property_stl.h`（算术 / string / json / array / vector / unordered_map / map）+ `property_bag.h` / `property_slots.h` / `property_vec.h` + Meta 生成的自定义 property 类。

---

## 2. 标量（Scalar）

C++：`prop_record_proxy<T>` 对 `std::is_arithmetic_v<T>`、`std::string`、`json` 特化；命令主要是 `set` / `clear`。

| DSL | 含义 | C++ 现状 / 落点 | Lua 落点 | DSL 状态 | 备注 |
|-----|------|-----------------|----------|----------|------|
| `bool` | 布尔 | `bool` ✅ | `boolean` | ✅ 纳入 | |
| `int` | 默认有符号 32 位整数 | `int` / 建议生成 `int32_t` ✅ | `number`（按整数用） | ✅ 纳入 | 书写糖；IR 可记为 int32 |
| `int8` | 8 位有符号 | `int8_t` / `char`（算术）✅ 库层通 | `number` | ⏳ 后期 | 现示例未用；库已支持算术 |
| `int16` | 16 位有符号 | `int16_t` / `short` ✅ | `number` | ⏳ 后期 | 同上 |
| `int32` | 32 位有符号 | `int` / `int32_t` ✅ | `number` | ✅ 纳入 | |
| `int64` | 64 位有符号 | `int64_t` / `long long` ✅ | `number` 或整数字符串策略 | ✅ 纳入 | Lua 大整数需约定（见 §7） |
| `uint8` | 8 位无符号 | 算术 ✅ | `number` | ⏳ 后期 | |
| `uint16` | 16 位无符号 | 算术 ✅ | `number` | ⏳ 后期 | |
| `uint32` | 32 位无符号 | 算术 ✅ | `number` | ✅ 纳入 | |
| `uint64` | 64 位无符号 | 算术 ✅ | 同 int64 | ✅ 纳入 | |
| `float` | IEEE754 单精度 | `float` ✅ | `number` | ✅ 纳入 | |
| `double` | 双精度 | `double` ✅ | `number` | ✅ 纳入 | |
| `string` | UTF-8 文本 | `std::string` ✅ | `string` | ✅ 纳入 | |
| `bytes` | 原始字节 | 无专用 Proxy；可用 `string`/`json` 凑 | `string` | ⏳ 后期 | 若需要再单列 wire |
| `json` | 任意 JSON 值 | `nlohmann::json` ✅ 有特化 | table / 透传 | ⏳ 后期 | 灵活但难静态对拍；默认不进首版 DSL |
| `enum` | 枚举 | 底层多为整型算术 ✅ | `number` | ⏳ 后期 | DSL 可写 `enum Name {…}` 再当 int |

**首版 DSL 标量建议锁定：**  
`bool` · `int` · `int32` · `int64` · `uint32` · `uint64` · `float` · `double` · `string`

---

## 3. 简单容器（元素仅为标量）

### 3.1 对照表

| DSL | wire 语义 | C++ 现状 | C++ 生成落点 | Lua 落点 | DSL 状态 |
|-----|-----------|----------|--------------|----------|----------|
| `array<T, N>` | 定长 N 个标量；可整表 `set`、按下落标 `item_change` | `std::array<T,N>` ✅ | 同左 | 序列表，长度恒为 N；**存储 1-based，协议下标 0-based** | ✅ 纳入 |
| `list<T>` | 变长标量序列；`set/clear/push/pop/erase/item_change/add…` | `std::vector<T>` ✅ | 同左 | 序列表 | ✅ 纳入 |
| `dict<K, V>` | 关联表；`set/clear/add/erase` | `std::unordered_map<K,V>` ✅ | **默认** `unordered_map` | 哈希表（非数组部分） | ✅ 纳入 |
| `omap<K, V>` | 有序关联表 | `std::map<K,V>` ✅ 有特化 | `std::map` | 可用有序数组-of-pairs 或有序遍历表 | ⏳ 后期 | 与 `dict` 线语义不同（有序）；首版可不开放 |
| `map<K, V>` | — | — | → 归一为 `dict` | → `dict` | ✅ 别名 | 仅解析别名，IR 写 `dict` |

### 3.2 元素 / 键类型约束（首版）

| 容器 | `T` / `K` / `V` 允许 |
|------|----------------------|
| `array<T, N>` | `T` ∈ 首版标量 |
| `list<T>` | `T` ∈ 首版标量 |
| `dict<K, V>` | `K` ∈ `{string, int, int32, int64, uint32, uint64}`；`V` ∈ 首版标量 |

**不支持（与现库习惯一致）：** `list<bag<…>>`、`dict<string, Item>` 等——复杂结构走 §4。

### 3.3 IR `wire_kind` 与现生成器字符串

| DSL | 建议 IR wire_kind | 现 C++ Meta 猜测名 | 说明 |
|-----|-------------------|-------------------|------|
| `array<…>` | `array` | `array` | 一致 |
| `list<…>` | `list` | 现为 `vector` | emitter 映射：`list` → C++ `vector` / 旧 Lua `vector` 分支 |
| `dict<…>` | `dict` | 现为 `map` | emitter 映射：`dict` → 旧 `map` 分支 |

---

## 4. 复杂容器（结构体元素）

| DSL | wire 语义 | C++ 现状 | C++ 落点 | Lua 落点 | DSL 状态 |
|-----|-----------|----------|----------|----------|----------|
| `bag<Item>` | 按 **id** 索引；`add/erase/item_change/…` | `property_bag<Item>` ✅ | 同左 | `{ items=[], id_to_idx={} }`（或等价） | ✅ 纳入 |
| `slots<Item>` | 固定格；`resize/swap/move/add/erase/item_change` | `property_slots<Item>` ✅ | 同左 | `{ size, by_slot, by_id }` | ✅ 纳入 |
| `vec<Item>` | 有序复杂记录；`push/insert/erase/item_change` | `property_vec<Item>` ✅ | 同左 | 序列表，元素为 item table | ✅ 纳入 |

**元素类种类（DSL 声明 Item 自身时用）：**

| DSL kind | 隐式字段 | C++ 基类现状 | Lua item 约定 |
|----------|----------|--------------|---------------|
| `bag_item` | `id` @ index **0** | `property_bag_item<Key>` ✅ | 字段 `id` |
| `slot_item` | `id` @0，`slot` @1 | `property_slot_item<Key>` ✅ | 字段 `id`、`slot` |
| `vec_item` | 无 | `property_vec_item` ✅ | 无隐式 id |
| `entity` | 无 | 普通 `Meta(property)` 根类 ✅ | 根 table |
| `object` | 无 | 嵌套 property 类 ✅（`wire_kind=object`） | 嵌套 table |

`key <type>`（仅 bag_item / slot_item）：对应 C++ 基类模板参数；Lua 中 `id` 的类型与此一致。

---

## 5. 嵌套对象

| DSL | C++ 现状 | Lua | DSL 状态 |
|-----|----------|-----|----------|
| `object<T>` | 字段类型为带 `Meta(property)` 的类，走 `has_property_interface` + 子 Proxy ✅ | 嵌套 META + 子路径 offset | ✅ 纳入（可次于 bag 优先实现） |

与 `bag`/`vec` 区别：`object` 是 **单份子对象**，不是集合。

---

## 6. 一张总表（设计评审用）

| # | DSL 类型 | C++ 现状 | C++ 落点 | Lua 落点 | 首版 |
|---|----------|----------|----------|----------|------|
| 1 | `bool` | ✅ | `bool` | `boolean` | ✅ |
| 2 | `int` / `int32` | ✅ | `int32_t` | `number` | ✅ |
| 3 | `int64` | ✅ | `int64_t` | `number`* | ✅ |
| 4 | `uint32` / `uint64` | ✅ | 对应 | `number`* | ✅ |
| 5 | `float` / `double` | ✅ | 对应 | `number` | ✅ |
| 6 | `string` | ✅ | `std::string` | `string` | ✅ |
| 7 | `array<T,N>` | ✅ `std::array` | 同左 | 定长序列 | ✅ |
| 8 | `list<T>` | ✅ `std::vector` | 同左 | 变长序列 | ✅ |
| 9 | `dict<K,V>` | ✅ `unordered_map` | 同左 | 哈希表 | ✅ |
| 10 | `bag<Item>` | ✅ | `property_bag` | bag 表 | ✅ |
| 11 | `slots<Item>` | ✅ | `property_slots` | slots 表 | ✅ |
| 12 | `vec<Item>` | ✅ | `property_vec` | 记录序列 | ✅ |
| 13 | `object<T>` | ✅ | 嵌套 property | 嵌套表 | ✅ |
| 14 | `map<K,V>` | — | → `dict` | → `dict` | 别名 |
| 15 | `omap<K,V>` | ✅ `std::map` | `std::map` | 有序表 | 后期 |
| 16 | `json` | ✅ | `json` | 透传 | 后期 |
| 17 | `int8`/`int16`/… | ✅ 算术通 | 对应 | `number` | 后期 |
| 18 | `bytes` / `enum` | 部分 | 待定 | 待定 | 后期 |

\* Lua `int64`/`uint64`：见下一节。

---

## 7. 跨语言注意点（Record/Replay 要对齐的）

| 点 | 约定 |
|----|------|
| 下标 | 协议与 mutate **一律 0-based**；Lua 序列表内部可用 1-based，进出线转换 |
| 定长 `array` | N 写进 DSL；Replay `set` 长度不符应失败 |
| `dict` vs `omap` | 首版只保证 **无序关联**（`dict`）；不依赖遍历顺序对拍 |
| 大整数 | JSON 对拍以 number 能精确表示的整数为主；超出 Number 安全范围的 `int64` 策略（字符串 / 库）后期单独立项 |
| `list` IR 名 | 对外 DSL=`list`；兼容旧 Lua runtime 的 `wire_kind=="vector"` 时 emitter 可双写或适配层转换 |
| 复杂元素 | 禁止 `list<object>` 捷径；用 `vec`/`bag`/`slots` |

---

## 8. 与 rpg 示例字段的对应

| 字段 | DSL | C++ 今日 | Lua |
|------|-----|----------|-----|
| `nickname` | `string` | `std::string` | `string` |
| `hp` / `level` / `gold` | `int` | `int` | `number` |
| `pos` | `array<float, 3>` | `std::array<float,3>` | 长度 3 序列 |
| `tags` | `list<string>` | `std::vector<std::string>` | 序列 |
| `attrs` | `dict<string, int>` | `unordered_map<string,int>` | 哈希表 |
| `inventory` / `buffs` | `bag<…>` | `property_bag` | bag |
| `equipment` | `slots<EquipItem>` | `property_slots` | slots |
| `login_history` | `vec<LoginRecord>` | `property_vec` | 记录序列 |

---

## 9. 结论（首版锁定）

**纳入首版 DSL 的类型：**

```text
标量:   bool  int  int32  int64  uint32  uint64  float  double  string
简单:   array<T, N>  list<T>  dict<K, V>     （map 为 dict 别名）
复杂:   bag<I>  slots<I>  vec<I>  object<T>
种类:   entity  bag_item  slot_item  vec_item  object
```

**明确后期 / 不进首版：** `json`、`omap`、窄整数、`bytes`、`enum` DSL 语法。

以上覆盖当前 C++ 库的主路径能力；Lua 侧用中立 wire 名落地，避免 STL 口音。
