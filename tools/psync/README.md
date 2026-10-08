# psync（Go）

`.psync` DSL → IR / schema / lua / proto / C++（复用 `meta/mustache`）。

```bash
go build -o psync ./cmd/psync
./psync check   path/to/player.psync --root <repo>
./psync compile path/to/player.psync -o /tmp/ir --root <repo>
./psync emit    path/to/player.psync -o /tmp/out --root <repo>
# → out/{cpp,schema,lua,proto}/
go test ./...
```

CMake 目标：`psync_tool` → `${CMAKE_BINARY_DIR}/bin/psync`。

后续仓库工具默认放在 `tools/<name>/`（Go module + `cmd/<name>`）。
