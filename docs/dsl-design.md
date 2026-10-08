# 属性 DSL 设计（`.psync`）

> **目标：** 简洁、清晰、功能完善——能完整表达当前 `rpg_player` 模型，并作为 C++/Lua Record·Replay 的唯一合同源。  
> **配套：** [dsl-multi-runtime.md](./dsl-multi-runtime.md)、[compatibility.md](./compatibility.md)。  
> **状态：** 设计稿；语法以本文为准，实现可后置。

---

## 1. 设计原则

| 原则 | 含义 |
|------|------|
| **声明式** | 只描述字段/形状/可见性，不写业务逻辑 |
| **index 即协议** | 字段号显式写出，禁止依赖声明顺序隐式编号 |
| **一种类型一套词** | `array` / `list` / `dict` / `bag` / `slots` / `vec`（线语义，非宿主 STL） |
| **可读优先** | 一眼能看出「谁、什么类型、谁能看见」 |
| **可 diff** | 文本稳定、适合进 Git；废弃用 `deprecated` / `reserved`，不删号 |

**非目标：** 表达式、函数、继承业务类、RPC、ORM。

---

## 2. 文件与单元

| 项 | 约定 |
|----|------|
| 扩展名 | `.psync` |
| 编码 | UTF-8 |
| 注释 | `#` 行注释；`//` 行注释（二选一实现时都认，推荐 `#`） |
| 一个文件 | 可含多个 `flags` / `class`；用 `import "other.psync"` 拆分 |
| 大小写 | 类型关键字小写；类名 / flag 名 Pascal 或 snake 均可，**生成后保持原样** |

推荐目录：

```text
dsl/
  flags.psync
  items.psync
  player.psync
```

---

## 3. 语法总览

```text
import "path.psync"

flags <Name> { ... }

namespace <dotted.name>

using flags <Name>

<root_or_item_kind> <ClassName> {
  version <uint>
  ...kind-specific headers...
  <index>: <name> <type> [= <default>] [<flag>, ...] [deprecated]
  reserved <index> [, <index> | <index> to <index>] ...
}
```

### 3.1 类种类（kind）

| 关键字 | 含义 | 隐式字段（占用 index，勿再声明） |
|--------|------|----------------------------------|
| `entity` | 根对象（如 Player） | 无 |
| `bag_item` | `property_bag` 元素 | `id` @ **0**（类型由 `key`） |
| `slot_item` | `property_slots` 元素 | `id` @ **0**，`slot` @ **1** |
| `vec_item` | `property_vec` 元素 | 无（业务字段从 0 起） |
| `object` | 嵌套属性对象（可选，后期） | 无 |

`bag_item` / `slot_item` 必须声明：

```text
key <scalar_type>    # id 的类型，如 int / int64 / string
```

---

## 4. Flag 定义

```text
flags RpgFlags {
  bit save_db    = 0
  bit sync_self  = 1
  bit sync_ghost = 2
  bit sync_other = 3

  alias sync_clients = sync_self | sync_other
  alias mask_all     = *
}
```

| 句法 | 含义 |
|------|------|
| `bit name = N` | `1 << N`，N ∈ [0, 63] |
| `alias name = a \| b \| …` | 组合名，字段注解可用 |
| `alias name = *` | 全 1（仅特殊用途） |

字段上的 `[sync_clients, save_db]` 解析为 alias/bit 的按位或。  
`using flags RpgFlags` 后，本文件字段括号里的名字相对该枚举解析。

---

## 5. 类型系统（线格式中立，非某门语言口音）

> **完整对照表（C++ 现状 × DSL × Lua、首版范围）见 [dsl-types.md](./dsl-types.md)。** 本节为语法侧摘要。

DSL 类型描述的是 **同步线语义（wire）**，不是 C++ STL，也不是 Lua table 字面写法。  
各语言 Record/Replay 只是把同一 wire 落到本地存储：

| 原则 | 说明 |
|------|------|
| 统一形态 | 容器一律 `name<...>`，不用 `float[3]` 这种 C 数组糖 |
| 命名避开宿主梗 | 不用 `std::vector` / `unordered_map`；`vec` **专指**有序复杂记录容器 |
| 简单 vs 复杂 | 标量序列用 `array`/`list`/`dict`；复杂元素用 `bag`/`slots`/`vec` |

### 5.1 标量（跨语言同一套）

| DSL | 含义 | C++ 落点 | Lua 落点 | Proto |
|-----|------|----------|----------|-------|
| `bool` | 布尔 | `bool` | `boolean` | `bool` |
| `int` | 默认有符号 32 | `int32_t` | `number`（整数约定） | `int32` |
| `int32` / `int64` | 明确宽度 | 对应 | `number` / 需大整数策略时用字符串可选 | `int32`/`int64` |
| `uint32` / `uint64` | 无符号 | 对应 | 同上 | `uint32`/`uint64` |
| `float` / `double` | 浮点 | 对应 | `number` | `float`/`double` |
| `string` | 文本 | `std::string` | `string` | `string` |

### 5.2 简单容器（元素仅为标量）

一律：

```text
array<T, N>     # 定长序列，长度 N 是协议的一部分
list<T>         # 变长序列
dict<K, V>      # 关联表（键值均为标量）
```

| DSL | wire_kind | 同步语义 | C++ Record/Replay | Lua Record/Replay |
|-----|-----------|----------|-------------------|-------------------|
| `array<float, 3>` | `array` | 固定 N 个标量；可 `item_change(i, v)` | `std::array<T,N>` | 长度 N 的序列表（1-based 存储，线上下标仍 0-based） |
| `list<string>` | `list` | 变长标量序列；push/pop/erase… | `std::vector<T>` | 序列表 |
| `dict<string, int>` | `dict` | 键值关联；add/erase… | `std::unordered_map<K,V>` | 哈希表（非序列部分） |

**别名（可选，解析器可认，IR 归一）：**

| 别名 | 规范名 | 说明 |
|------|--------|------|
| `map<K,V>` | `dict<K,V>` | 兼容习惯写法；文档与样例以 `dict` 为准 |
| ~~`T[N]`~~ | `array<T, N>` | **不推荐**；易读成 C 数组，已废弃于 DSL |

`list` 的 wire_kind 在 IR 里可用 `list`（新）或继续映射到现实现的 `vector` 字符串，由 emitter 适配；**对外 DSL 只写 `list`**。

### 5.3 复杂容器（元素为 `*_item` / `object`）

| DSL | wire_kind | 语义 | 元素约束 |
|-----|-----------|------|----------|
| `bag<Item>` | `bag` | 按 id 索引的包 | `bag_item` |
| `slots<Item>` | `slots` | 有格栏 | `slot_item` |
| `vec<Item>` | `vec` | 有序复杂记录 | `vec_item` |
| `object<T>` | `object` | 嵌套属性对象 | `object`/`entity` |

注意：**`vec<>` ≠ `list<>`**  
- `list<string>`：一串标量  
- `vec<LoginRecord>`：一串结构体，可对某条做 `item_change`

### 5.4 为何不用 `float[3]` / `map<…>` 当主写法

| 旧写法 | 问题（双运行时） |
|--------|------------------|
| `float[3]` | C 口音；Lua 无定长数组类型，读者易以为是宿主语法 |
| `map<K,V>` | 易联想到 C++ `std::map` 有序表；Lua 侧是 hash |
| `list` vs `vector` 混用 | 与 `vec<item>` 撞名风险 |

统一为 `array` / `list` / `dict` 后：读 DSL 只关心线语义；C++/Lua emitter 各自选存储，对拍比的是 mutate 与 sync view，不是 `std::` 类型名。

### 5.5 限制

- `array` / `list` / `dict` 的 `T`/`K`/`V` **仅标量**（复杂结构 → `bag`/`slots`/`vec`/`object`）。  
- 单类字段 index ∈ `[0, 254]`。  
- 嵌套路径深度 ≤ 8。

### 5.6 默认值

```text
1: hp int = 100 [sync_clients]
0: nickname string = "" [sync_clients]
4: pos array<float, 3> = {0, 0, 0} [sync_clients]
```

- 标量：`0` / `0.0` / `true` / `"..."`  
- `array`：`{a, b, c}`（长度必须等于 N）  
- `list` / `dict` / `bag` / … 默认空；省略 `=` 则用零值  

---

## 6. 字段声明

```text
<index>: <name> <type> [= <default>] [<flag>, ...] [deprecated ["reason"]]
```

示例：

```text
3: gold int = 0 [save_db]
2: name string [save_db] deprecated "use title"
```

| 部分 | 规则 |
|------|------|
| `index` | 显式；同类内唯一；废弃后仍占用 |
| `name` | 标识符；生成 API 名（C++ 可加 `m_` 前缀策略） |
| `flags` | 至少一个推荐；允许空 `[]` 表示仅本地（慎用） |
| `deprecated` | 保留 index；生成物可拒绝 Record 写入，Replay 仍识别 |
| `reserved` | 仅占号，无名字 |

```text
reserved 11, 12
reserved 20 to 25
```

---

## 7. 版本与命名空间

```text
namespace spiritsaway.rpg_example

entity Player {
  version 1
  ...
}
```

| 字段 | 含义 |
|------|------|
| `namespace` | 生成 C++ 命名空间 / Lua 模块前缀 / proto package 片段 |
| `version` | 该类 `schema_version`；存档/连接门闩；**破坏性变更必须递增** |

兼容规则同 [compatibility.md](./compatibility.md)：只追加 index、不复用、不改同号语义。

---

## 8. 完整示例（覆盖 rpg_player）

### `dsl/flags.psync`

```text
flags RpgFlags {
  bit save_db    = 0
  bit sync_self  = 1
  bit sync_ghost = 2
  bit sync_other = 3

  alias sync_clients = sync_self | sync_other
}
```

### `dsl/items.psync`

```text
import "flags.psync"

namespace spiritsaway.rpg_example
using flags RpgFlags

bag_item Item {
  version 1
  key int
  # id @0
  1: count int = 0 [sync_clients]
  2: name  string = "" [save_db]
}

bag_item Buff {
  version 1
  key int
  1: level     int   = 0 [sync_clients]
  2: expire_ts float = 0 [sync_clients]
}

slot_item EquipItem {
  version 1
  key int
  # id @0, slot @1
  2: enhance int = 0 [sync_clients]
  3: name    string = "" [sync_clients]
}

vec_item LoginRecord {
  version 1
  0: login_ts  float = 0 [sync_clients]
  1: logout_ts float = 0 [sync_clients]
  2: ip        string = "" [save_db]
}
```

### `dsl/player.psync`

```text
import "items.psync"

namespace spiritsaway.rpg_example
using flags RpgFlags

entity Player {
  version 1

  0: nickname string = "" [sync_clients]
  1: hp       int    = 100 [sync_clients]
  2: level    int    = 1 [sync_clients]
  3: gold     int    = 0 [save_db]

  4: pos   array<float, 3>     [sync_clients]
  5: tags  list<string>        [sync_clients]
  6: attrs dict<string, int>   [sync_clients]

  7: inventory      bag<Item>          [sync_clients, save_db]
  8: buffs          bag<Buff>          [sync_clients, save_db]
  9: equipment      slots<EquipItem>   [sync_clients, save_db]
  10: login_history vec<LoginRecord>   [sync_clients, save_db]
}
```

以上与现有示例字段 index / flag / 容器种类 **一一对应**。

---

## 9. 语义校验（编译期）

生成器在 DSL→IR 时必须检查：

| ID | 规则 |
|----|------|
| V1 | index 不冲突；不落入 kind 隐式保留位（bag 的 0；slot 的 0/1） |
| V2 | `bag<T>` 的 T 是 `bag_item`；`slots`/`vec` 同理 |
| V3 | flag 名可解析为 bit 或 alias |
| V4 | `array<T, N>` 的 N ≥ 1；`list`/`dict` 的键值均为标量 |
| V5 | `deprecated` / `reserved` 的 index 不可再声明活跃字段 |
| V6 | `version` ≥ 1；同 class 改 wire/item 同 index → 错误（应新 index + deprecated 旧） |
| V7 | import 无环；类名在 namespace 内唯一 |

---

## 10. 降到 IR（与生成器衔接）

每个 class 降为 JSON IR（示意）：

```json
{
  "name": "Player",
  "kind": "entity",
  "namespace": "spiritsaway.rpg_example",
  "schema_version": 1,
  "fields": [
    {
      "index": 1,
      "name": "hp",
      "type": { "kind": "scalar", "name": "int" },
      "wire_kind": "number",
      "default": 100,
      "flags": ["sync_clients"],
      "deprecated": false
    },
    {
      "index": 7,
      "name": "inventory",
      "type": { "kind": "bag", "item": "Item" },
      "wire_kind": "bag",
      "flags": ["sync_clients", "save_db"]
    }
  ],
  "reserved": [],
  "flags_ref": "RpgFlags"
}
```

现有 `ClassModel` / `classify_field` 可改为 **消费 IR**，不再从 C++ 拼写猜类型。

---

## 11. 生成映射（功能闭环）

| DSL | C++ | Lua | schema / proto |
|-----|-----|-----|----------------|
| `entity`/`*_item` | 类 + 基类 | `*_record` / `*_replay` META | class + kind |
| `index: name` | `m_name` + `index_for_name` | `INDEX.name` | fields[].index |
| `[flags]` | `flag_for_*` | `flags = {...}` | fields[].flags |
| `bag`/`slots`/`vec` | `property_*` | wire_kind + item_meta | item_class |
| `array`/`list`/`dict` | `array`/`vector`/`unordered_map` | 序列表 / 哈希表 | wire_kind |
| `version` | 常量 | `SCHEMA_VERSION` | schema_version |
| `deprecated` | 保留成员或注解 | Replay 只读分支 | deprecated: true |
| `reserved` | （无成员） | （无） | reserved + proto reserved |

---

## 12. 有意不做（保持简洁）

| 不做 | 原因 |
|------|------|
| 字段表达式 / 校验公式 | 属业务层 |
| 多继承、Mixin | 同步路径复杂 |
| 泛型 class | 生成与对拍成本高 |
| 匿名嵌套 struct | 改为具名 `object`/`vec_item` |
| 自动分配 index | 违反「index 即协议」、难 diff |

后续若需要「只追加时省略 index」，可加工具 **建议下一号**，但仍写入文件为显式数字。

---

## 13. 语法速查卡

```text
flags F { bit a = 0; alias clients = a | b }

entity P {
  version 1
  0: name string [clients]
  1: hp int = 100 [clients]
  2: bag bag<Item> [clients, save_db]
  reserved 3 to 5
}

bag_item Item  { version 1; key int; 1: count int [clients] }
slot_item E    { version 1; key int; 2: enhance int [clients] }
vec_item Rec   { version 1; 0: ts float [clients] }
```

类型：`bool int int32 int64 uint32 uint64 float double string`  
简单容器：`array<T, N>  list<T>  dict<K, V>`  
复杂容器：`bag<I>  slots<I>  vec<I>  object<T>`  
（`map<>` 可作为 `dict<>` 别名；不要用 `T[N]`）

---

## 14. 实施

语法与样例已定稿（`dsl/*.psync`）。**实施阶段、改动面与验收**见 [dsl-implementation-plan.md](./dsl-implementation-plan.md)（S0→S8）。
