#!/usr/bin/env bash
# One-shot DSL / IR / emit regression suite. See docs/dsl-test.md
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PYTHONPATH="$ROOT/tools"

echo "==> [1/4] psync unittest (golden + emit + native-wire)"
python3 -m unittest discover -s tools -p 'test_*.py' -v

echo "==> [2/4] psync check / compile smoke"
python3 -m psync check dsl/player.psync --root .
python3 -m psync compile dsl/player.psync -o /tmp/psync-ir-ci --root . --no-bundle >/dev/null

echo "==> [3/4] psync emit smoke"
python3 -m psync emit dsl/player.psync -o /tmp/psync-emit-ci --root . >/dev/null
python3 -m psync emit dsl/player.psync -o /tmp/psync-emit-native --root . --native-wire >/dev/null

META_SCHEMA="$ROOT/build/examples/rpg_player/generated/schema/Player.schema.json"
if [[ -f "$META_SCHEMA" ]]; then
  echo "==> [4/4] Meta artifacts present — emit vs Meta already covered by unittest"
else
  echo "==> [4/4] SKIP emit-vs-Meta detail (build Meta artifacts first:"
  echo "         cmake --build build --target rpg_player_generate)"
fi

echo
echo "ALL DSL CHECKS PASSED"
