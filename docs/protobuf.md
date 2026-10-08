# Protobuf 生成约定

详见总方案 [evolution-plan.md](./evolution-plan.md)。构建期 Meta 会生成：

| 路径 | 内容 |
|------|------|
| `generated/proto/property_mutate.proto` | 通用 `MutateMsg` / `MutateBatch` / `PropertyCmd` |
| `generated/proto/<Class>.proto` | `<Class>Snapshot`；slot 类另含 `<Class>SlotsSnapshot` |

## 字段号

- Snapshot 中 **`schema_version = 1000`**（高号）。  
- 业务字段：**proto field number = property `index` + 1**（Protobuf 不允许 field 0）。  
- bag 基字段：`id = 1`；slot 基字段：`id = 1`，`slot = 2`。  
- `property_slots` 字段类型为 `<Item>SlotsSnapshot`（`sz` + `data`），与 JSON encode 同形。  
- 废弃字段：不要复用号码；使用 `reserved`。

## 依赖与验收

- 引用其他 Snapshot 时生成 `import "<Item>.proto";`。  
- 本地验收：

```bash
cmake --build build --target rpg_player_proto_check
```

## 与 JSON 双轨

- v1 的 `MutateMsg.data` 为 UTF-8 JSON（与现有 any_container 载荷兼容）。  
- 全量存档优先用 Snapshot（JSON 或日后 PB）；增量继续可用 JSON 队列或 PB `MutateBatch`。  
- **P5** 再接 C++ PB 编解码；当前以 `protoc` 编译通过为 P2 验收。

## 兼容

见 [compatibility.md](./compatibility.md)。用 `scripts/diff_schema.py` 对比 `*.schema.json`，勿只改 proto。
