# 字段兼容约定

> 与 [evolution-plan.md](./evolution-plan.md) 配套。属性字段的 **index 即协议**；无热更客户端下，C++ / schema / proto / 纯 Lua 必须 **同版本打包**。

## 硬性规则

| 编号 | 规则 | 说明 |
|------|------|------|
| R1 | **只追加** | 新字段只能加在类定义末尾，占用新的 `index_for_*` |
| R2 | **不回收编号** | 废弃字段不得删除后让后续字段下标前移；保留占位成员或 schema/`reserved` |
| R3 | **不改同 index 语义** | 禁止同 index 改类型、改 bag/slots/vec、改元素类型 |
| R4 | **schema_version** | 存档与连接携带版本；与包内生成物不一致则拒载/拒连 |
| R5 | **整包同版** | 客户端无热更：禁止只更新 Lua/脚本而不更新 native/schema |

## 允许 / 禁止对照

| 变更 | 是否允许 | 做法 |
|------|----------|------|
| 末尾新增 `m_xxx` | ✅ | index 递增；升 `schema_version`（建议） |
| 删除中间字段 | ❌ | 改为废弃：保留成员或占位 + schema `deprecated` |
| 重命名字段但保持 index | ⚠️ | 仅当线格式与存档仍按 index 解释；对外名变更需文档；CI 默认可标警告 |
| `int` → `string` 同 index | ❌ | 新字段追加，旧字段废弃 |
| `property_bag` → `property_slots` | ❌ | 破坏变更，新版本 + migration |
| 只改 flag（如增加 sync） | ⚠️ | 不改 index 一般可接受；需评估旧客户端行为 |
| 调整源码中字段书写顺序导致 index 重排 | ❌ | 生成顺序即定义顺序；插入中间等同破坏 |

## Schema 合同

构建期 Meta 生成 `*.schema.json`（见 `generated/**/*.schema.json`），作为：

- CI `scripts/diff_schema.py` 对比基线的输入  
- Protobuf 字段号、纯 Lua `index` 分发的共同来源说明  

对比失败条件（默认）：

- 缺少旧 index  
- 同 index 的 `name` / `wire_kind` / `item_class` 变化  

## 废弃字段推荐写法

```cpp
// 废弃：不要删除，以免后面字段 index 前移
Meta(property(save_db)) int m_legacy_score = 0; // deprecated: use m_score_v2
Meta(property(sync_clients)) int m_score_v2 = 0; // 新字段追加在后
```

Protobuf 侧对应 `reserved`；Lua 生成物可忽略 deprecated 的写入路径但保留 replay 分支（可选）。

## 版本门闩（无热更）

```text
客户端包内 SCHEMA_VERSION（生成进 Lua / 或原生常量）
        │
        ▼
收到 Snapshot / MutateBatch / 打开存档
        │
        ├─ version == 本地 → 继续 Replay
        └─ version != 本地 → 拒绝并提示更新客户端
```

## 开发检查清单

1. 新增字段是否只加在末尾？  
2. 是否跑过 meta 生成并提交/打包 schema？  
3. `diff_schema.py` 对比上一发布 tag 是否通过？  
4. C++、proto、Lua 是否同一构建号制品？  

## 相关文档

- [evolution-plan.md](./evolution-plan.md) — 总方案  
- [protobuf.md](./protobuf.md) — 字段号与线格式  
- [lua-sync.md](./lua-sync.md) — 纯 Lua Replay  
