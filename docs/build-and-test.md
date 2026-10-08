# 编译与测试

本文整理本仓库的依赖安装、CMake 配置、代码生成、单元测试与 RPG 示例的常用命令。

## 1. 依赖

| 依赖 | 用途 |
|------|------|
| C++17 编译器 | 编译库与测试 |
| [nlohmann/json](https://github.com/nlohmann/json) | JSON |
| [huangfeidian/any_container](https://github.com/huangfeidian/any_container) | encode / decode |
| [huangfeidian/meta](https://github.com/huangfeidian/meta) | 解析 `Meta(property)` |
| Clang / libclang | meta 代码生成 |
| fmt、spdlog | meta 库依赖 |

下面用「本地 prefix」示例（可换成系统包或 vcpkg）。假设：

```bash
export DEPS=/path/to/deps-install          # 例如 .../game-server/_deps/install
export REPO=/path/to/property_sync
cd "$REPO"
```

```bash
cd /home/game/open-source/game-server/property_sync
DEPS=/home/game/open-source/game-server/_deps/install
cmake -S . -B build \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_PREFIX_PATH="$DEPS;$DEPS/lib64/cmake;/usr/lib64/llvm21/lib64/cmake" \
  -DClang_DIR=/usr/lib64/llvm21/lib64/cmake/clang \
  -DLLVM_DIR=/usr/lib64/llvm21/lib64/cmake/llvm \
  -DWITH_TEST=ON \
  -DWITH_EXAMPLES=ON
cmake --build build --target rpg_player_example -j"$(nproc)"
```

### 1.1 拉取并安装依赖（可选）

```bash
mkdir -p ../_deps && cd ../_deps
git clone --depth 1 https://github.com/nlohmann/json.git
git clone --depth 1 https://github.com/fmtlib/fmt.git
git clone --depth 1 https://github.com/gabime/spdlog.git
git clone --depth 1 https://github.com/huangfeidian/any_container.git
git clone --depth 1 https://github.com/huangfeidian/meta.git

# nlohmann_json
cmake -S json -B build/json -DCMAKE_INSTALL_PREFIX="$DEPS" -DJSON_BuildTests=OFF
cmake --build build/json -j && cmake --install build/json

# fmt
cmake -S fmt -B build/fmt -DCMAKE_INSTALL_PREFIX="$DEPS" -DFMT_TEST=OFF -DFMT_DOC=OFF
cmake --build build/fmt -j && cmake --install build/fmt

# spdlog（使用外部 fmt）
cmake -S spdlog -B build/spdlog \
  -DCMAKE_INSTALL_PREFIX="$DEPS" -DCMAKE_PREFIX_PATH="$DEPS" \
  -DSPDLOG_BUILD_EXAMPLE=OFF -DSPDLOG_FMT_EXTERNAL=ON
cmake --build build/spdlog -j && cmake --install build/spdlog

# any_container
cmake -S any_container -B build/any_container \
  -DCMAKE_INSTALL_PREFIX="$DEPS" -DCMAKE_PREFIX_PATH="$DEPS"
cmake --build build/any_container -j && cmake --install build/any_container

# meta（需要 ClangConfig）
cmake -S meta -B build/meta \
  -DCMAKE_INSTALL_PREFIX="$DEPS" \
  -DCMAKE_PREFIX_PATH="$DEPS;/usr/lib64/llvm21/lib64/cmake" \
  -DClang_DIR=/usr/lib64/llvm21/lib64/cmake/clang \
  -DLLVM_DIR=/usr/lib64/llvm21/lib64/cmake/llvm
cmake --build build/meta -j && cmake --install build/meta

cd "$REPO"
```

> Clang / LLVM 路径因发行版而异，可用 `find /usr -name ClangConfig.cmake 2>/dev/null` 查找。

## 2. 配置工程

```bash
cmake -S . -B build \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_PREFIX_PATH="$DEPS;$DEPS/lib64/cmake;/usr/lib64/llvm21/lib64/cmake" \
  -DClang_DIR=/usr/lib64/llvm21/lib64/cmake/clang \
  -DLLVM_DIR=/usr/lib64/llvm21/lib64/cmake/llvm \
  -DWITH_TEST=ON \
  -DWITH_EXAMPLES=ON
```

常用开关：

| 选项 | 默认 | 含义 |
|------|------|------|
| `WITH_TEST` | ON | 编译 `property_test` |
| `WITH_EXAMPLES` | ON | 编译 `rpg_player_example` |

## 3. 编译目标一览

| 目标 | 说明 |
|------|------|
| `generate_property_sync` | Meta 属性代码生成器 |
| `property_test` | 仓库单元测试（需先手动/半手动生成 inch） |
| `rpg_player_example` | RPG 示例（CMake 会自动跑生成器） |
| `rpg_player_generate` | 仅生成 RPG 示例的 inch 文件 |
| `rpg_player_generate_from_dsl` | DSL→IR→schema/lua/proto（`generated/from_dsl`） |
| `rpg_player_dsl_check` | DSL golden + emit≡Meta 语义对拍 |
| `rpg_player_lua_replay` | C++/Lua batch+snapshot+mixed 对拍 |
| `rpg_player_lua_record` | 纯 Lua Record（S5/S6） |
| `rpg_player_replay_json` | C++ Replay mutate JSON（S7） |
| `rpg_player_cross_matrix` | Record×Replay 交叉矩阵 |
| `rpg_player_proto_check` | protoc 编译检查生成 proto |

DSL 专项步骤见 **[dsl-test.md](./dsl-test.md)**。

```bash
# 只编生成器
cmake --build build --target generate_property_sync -j

# 编并跑 RPG 示例（推荐先验证环境）
cmake --build build --target rpg_player_example -j
./build/examples/rpg_player/rpg_player_example

# 编单元测试（见下一节：需先生成代码）
cmake --build build --target property_test -j
./build/test/property_test
```

## 4. 单元测试：`property_test`

`test/` 的 CMake **不会**自动调用生成器，需要先生成 `*.generated.inch` / `*.proxy.inch`。

### 4.1 准备 `test/config.json`

至少包含：

- `include_dirs`：本仓库 `include`、依赖头文件、`build/test/generated`
- `definitions`：务必带 Clang **`-resource-dir`**（否则基类解析失败，背包 proxy 签名错误）
- `mustache_folder` / `generated_folder`：必须以 `.` 开头的相对路径
- `namespace`：`spiritsaway::test`
- `flag_class`：`spiritsaway::property::test_property_flags`

查看本机 resource-dir：

```bash
clang++ -print-resource-dir
# 例如 /usr/lib/clang/21 或 /usr/bin/../lib/clang/21
```

示例片段：

```json
{
  "include_dirs": [
    "../include",
    "/usr/include",
    "<DEPS>/include",
    "<CLANG_RESOURCE_DIR>/include",
    "../build/test/generated"
  ],
  "src_file": "./generate_entry.cpp",
  "definitions": [
    "-x", "c++", "-std=c++17", "-fparse-all-comments",
    "-resource-dir", "<CLANG_RESOURCE_DIR>"
  ],
  "mustache_folder": "../meta/mustache",
  "generated_folder": "../build/test/generated",
  "namespace": "spiritsaway::test",
  "flag_class": "spiritsaway::property::test_property_flags"
}
```

### 4.2 生成 → 编译 → 运行

```bash
mkdir -p build/test/generated

# 先确保生成器已编译
cmake --build build --target generate_property_sync -j

# 在 test 目录执行生成（config 里路径相对 test/）
cd test
../build/meta/generate_property_sync ./config.json
cd ..

# 编译并运行
cmake --build build --target property_test -j
./build/test/property_test
```

判定：

- 输出中 **不应** 出现 `fail to relay`
- 成功时退出码应为 `0`；日志中不应出现 `fail to relay`

生成时 cwd 下可能出现体积很大的 `meta.log` / `type_info.json`，可删：

```bash
rm -f test/meta.log test/type_info.json
rm -f examples/rpg_player/meta.log examples/rpg_player/type_info.json
```

## 5. RPG 示例：`rpg_player_example`

示例已接入 CMake：构建目标时会自动生成 `Item` / `Buff` / `Player` 的代码。

```bash
cmake --build build --target rpg_player_example -j
./build/examples/rpg_player/rpg_player_example
```

成功时应看到 `[PASS] 观察者可见字段与服务器一致`，退出码 `0`。

仅重新生成代码：

```bash
# 若生成结果异常，可先清掉再编
rm -rf build/examples/rpg_player/generated
cmake --build build --target rpg_player_generate -j
```

CMake 会自动探测 `clang++ -print-resource-dir`；也可手动指定：

```bash
export CLANG_RESOURCE_DIR=/usr/lib/clang/21
cmake -S . -B build ...   # 重新 configure 后再生效
```

说明见 [`examples/rpg_player/README.md`](../examples/rpg_player/README.md)、[`game-example.md`](./game-example.md)。

## 6. 常见问题

### 6.1 `simple_bag_item.generated.inch: No such file`

未跑 meta 生成。按第 4 节先 `generate_property_sync` 再编 `property_test`。

### 6.2 proxy 构造函数「expects 4 arguments, 5 provided」

libclang 解析不完整（缺 `-resource-dir`），`has_base_class` / item 特化丢失。在 `config.json` 的 `definitions` 里补上 resource-dir，删掉 `generated/` 后重生。

### 6.3 `find_package(any_container / meta / Clang)` 失败

检查 `CMAKE_PREFIX_PATH`、`Clang_DIR`、`LLVM_DIR` 是否指向实际安装前缀。

### 6.4 生成很慢 / `meta.log` 巨大

解析标准库时会打大量日志，属正常现象；生成结束后可删除 `meta.log`。

## 7. 最短验证清单

环境就绪后，用这两条确认「能生成 + 能同步」：

```bash
cmake --build build --target rpg_player_example -j && \
  ./build/examples/rpg_player/rpg_player_example

# 以及（配置好 test/config.json 并生成后）
cmake --build build --target property_test -j && \
  ./build/test/property_test 2>&1 | grep -c 'fail to relay' || true
# 期望输出 0（或 grep 无匹配）
```
