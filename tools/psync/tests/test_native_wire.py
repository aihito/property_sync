"""Native wire_kind list/dict emission (S4)."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from psync.compile import compile_file
from psync.emit import build_emit_class, write_emitted

REPO = Path(__file__).resolve().parents[3]
ENTRY = REPO / "dsl" / "player.psync"


class NativeWireTest(unittest.TestCase):
    def test_native_wire_kinds(self) -> None:
        unit, _ = compile_file(ENTRY, root=REPO)
        player = build_emit_class(unit.classes["Player"], legacy_wire=False)
        by_name = {f.name: f.wire_kind for f in player.fields}
        self.assertEqual(by_name["tags"], "list")
        self.assertEqual(by_name["attrs"], "dict")
        self.assertEqual(by_name["pos"], "array")
        self.assertEqual(by_name["inventory"], "bag")

    def test_legacy_still_maps(self) -> None:
        unit, _ = compile_file(ENTRY, root=REPO)
        player = build_emit_class(unit.classes["Player"], legacy_wire=True)
        by_name = {f.name: f.wire_kind for f in player.fields}
        self.assertEqual(by_name["tags"], "vector")
        self.assertEqual(by_name["attrs"], "map")

    def test_emit_native_schema(self) -> None:
        unit, _ = compile_file(ENTRY, root=REPO)
        with tempfile.TemporaryDirectory() as td:
            write_emitted(unit, Path(td), legacy_wire=False, copy_runtime=False)
            doc = json.loads((Path(td) / "schema" / "Player.schema.json").read_text())
            kinds = {f["name"]: f["wire_kind"] for f in doc["fields"]}
            self.assertEqual(kinds["tags"], "list")
            self.assertEqual(kinds["attrs"], "dict")


if __name__ == "__main__":
    unittest.main()
