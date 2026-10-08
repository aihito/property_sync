# Protobuf 生成约定

详见总方案 [evolution-plan.md](./evolution-plan.md)。构建期 `psync emit` / Meta 会生成：

| 路径 | 内容 |
|------|------|
| `generated/proto/<snake>.proto` | 消息 `<Class>`（PascalCase，与 DSL/C++ 对齐）；slot 类另含 `<Class>Slots` |
| `generated/proto/mutate.proto` | `PropertyCmd` / `MutateMsg` / `MutateBatch` |
| `generated/cpp/<Class>.h/.cpp` | 成员函数 `to_pb` / `from_pb`（`#if PROPERTY_SYNC_WITH_PROTOBUF`） |

**文件名**：snake_case（`Player` → `player.proto`，`LoginRecord` → `login_record.proto`）。  
**package / C++ 命名空间**：统一 `psync`（例：`psync.Player` / `psync::Player`）。

## 字段号

- 全量消息中 **`schema_version = 1000`**（高号）。  
- 业务字段：**proto field number = property `index` + 1**（Protobuf 不允许 field 0）。  
- bag 基字段：`id = 1`；slot 基字段：`id = 1`，`slot = 2`。  
- `property_slots` 字段类型为 `<Item>Slots`（`sz` + `data`），与 JSON encode 同形。  
- 废弃字段：不要复用号码；使用 `reserved`。

## Codec（JSON / Protobuf 并存）

头文件：[`include/property_codec.h`](../include/property_codec.h)。

| `codec_kind` | 实现 |
|--------------|------|
| `json` | 已有 `encode_with_flag` / `decode` |
| `protobuf` | `obj.to_pb(...)` → `SerializeToString`；`ParseFromString` → `obj.from_pb(...)` |
| `both` | 两条路径都跑，并对拍视图 |

**不再**使用 `JsonStringToMessage` 中转。`to_pb` 按 `property_flags` 过滤字段，与 JSON 视图语义对齐。

```bash
# Fedora: sudo dnf install protobuf-devel
cmake -S . -B build -DWITH_PROTOBUF=ON
cmake --build build --target channel_matrix_all -j
```

- 增量 mutate 队列仍为 JSON。  
- 未找到 libprotobuf 时仅 `codec_kind::json` 可用。

## 依赖与验收

```bash
cmake --build build --target channel_matrix_all -j
cmake --build build --target check_all -j
```

## 兼容

见 [compatibility.md](./compatibility.md)。用 `scripts/diff_schema.py` 对比 `*.schema.json`，勿只改 proto。
