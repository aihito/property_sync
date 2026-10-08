#!/usr/bin/env bash
# One-shot DSL / IR / emit regression suite. See docs/dsl-test.md
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

PSYNC="$ROOT/tools/psync"
BIN="${TMPDIR:-/tmp}/psync-ci"

echo "==> [1/4] Go psync unit tests (IR golden + cpp semantic)"
(cd "$PSYNC" && go test ./... -count=1)

echo "==> [2/4] psync check / compile smoke"
(cd "$PSYNC" && go build -o "$BIN" ./cmd/psync)
"$BIN" check dsl/player.psync --root .
"$BIN" compile dsl/player.psync -o /tmp/psync-ir-ci --root . --no-bundle >/dev/null

echo "==> [3/4] psync emit smoke"
"$BIN" emit dsl/player.psync -o /tmp/psync-emit-ci --root . >/dev/null
"$BIN" emit dsl/player.psync -o /tmp/psync-emit-native --root . --native-wire >/dev/null

META_INCH="$ROOT/examples/rpg_player/generated/Player.generated.inch"
if [[ -f "$META_INCH" ]]; then
  echo "==> [4/4] Meta inch present — cpp semantic covered by go test"
else
  echo "==> [4/4] SKIP inch detail (run rpg_player_generate first)"
fi

echo
echo "ALL DSL CHECKS PASSED"
