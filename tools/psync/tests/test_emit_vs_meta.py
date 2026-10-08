"""Compare IR emit path vs Meta-generated artifacts (semantic).

Requires existing build:
  build/examples/rpg_player/generated/{schema,lua,proto}
"""

from __future__ import annotations

import json
import re
import tempfile
import unittest
from pathlib import Path

from psync.compile import compile_file
from psync.emit import (
    lua_semantic,
    proto_semantic,
    schema_semantic,
    write_emitted,
)

REPO = Path(__file__).resolve().parents[3]
META = REPO / "build" / "examples" / "rpg_player" / "generated"
ENTRY = REPO / "dsl" / "player.psync"
CLASSES = ["Player", "Item", "Buff", "EquipItem", "LoginRecord"]


def _norm_lua(text: str) -> str:
    s = lua_semantic(text)
    # Meta mustache leaves trailing commas inside flag tables
    s = re.sub(r",\s*}", " }", s)
    s = re.sub(r",\s*]", " ]", s)
    # Collapse whitespace so one-line INDEX vs multi-line INDEX match
    s = re.sub(r"\s+", " ", s).strip()
    return s


class EmitVsMetaTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        if not (META / "schema" / "Player.schema.json").is_file():
            raise unittest.SkipTest(f"Meta artifacts missing under {META}")

    def test_schema_semantic(self) -> None:
        unit, _ = compile_file(ENTRY, root=REPO)
        with tempfile.TemporaryDirectory() as td:
            write_emitted(unit, Path(td))
            for name in CLASSES:
                got = schema_semantic(
                    json.loads((Path(td) / "schema" / f"{name}.schema.json").read_text())
                )
                want = schema_semantic(
                    json.loads((META / "schema" / f"{name}.schema.json").read_text())
                )
                self.assertEqual(got, want, msg=name)

    def test_lua_semantic(self) -> None:
        unit, _ = compile_file(ENTRY, root=REPO)
        with tempfile.TemporaryDirectory() as td:
            write_emitted(unit, Path(td))
            for name in CLASSES:
                got = _norm_lua((Path(td) / "lua" / f"{name}_meta.lua").read_text())
                want = _norm_lua((META / "lua" / f"{name}_meta.lua").read_text())
                self.assertEqual(got, want, msg=name)

    def test_proto_semantic(self) -> None:
        unit, _ = compile_file(ENTRY, root=REPO)
        with tempfile.TemporaryDirectory() as td:
            write_emitted(unit, Path(td))
            for name in CLASSES:
                got = proto_semantic((Path(td) / "proto" / f"{name}.proto").read_text())
                want = proto_semantic((META / "proto" / f"{name}.proto").read_text())
                self.assertEqual(got, want, msg=name)


if __name__ == "__main__":
    unittest.main()
