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

TD="$PSYNC/testdata"
ENTRY="$TD/dsl/player.psync"

echo "==> [2/4] psync check / compile smoke"
(cd "$PSYNC" && go build -o "$BIN" ./cmd/psync)
"$BIN" check "$ENTRY" --root "$TD"
"$BIN" compile "$ENTRY" -o /tmp/psync-ir-ci --root "$TD" --no-bundle >/dev/null

echo "==> [3/4] psync emit smoke"
# emit --root must be the repo (meta/mustache); check/compile use testdata root for IR source_file
"$BIN" emit "$ENTRY" -o /tmp/psync-emit-ci --root "$ROOT" >/dev/null
"$BIN" emit "$ENTRY" -o /tmp/psync-emit-native --root "$ROOT" --native-wire >/dev/null

META_INCH="$ROOT/examples/rpg_player/generated/Player.generated.inch"
if [[ -f "$META_INCH" ]]; then
  echo "==> [4/4] Meta inch present — cpp semantic covered by go test"
else
  echo "==> [4/4] SKIP inch detail (run rpg_player_generate first)"
fi

echo
echo "ALL DSL CHECKS PASSED"
