"""Golden IR regression — run: PYTHONPATH=tools python -m unittest psync.tests.test_golden"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from psync.compile import compile_file, write_ir

REPO = Path(__file__).resolve().parents[3]
GOLDEN = REPO / "testdata" / "ir"
ENTRY = REPO / "dsl" / "player.psync"


class GoldenIRTest(unittest.TestCase):
    def test_compile_matches_golden(self) -> None:
        unit, diags = compile_file(ENTRY, root=REPO)
        errors = [d for d in diags if d.level == "error"]
        self.assertEqual(errors, [], msg=str(errors))

        with tempfile.TemporaryDirectory() as td:
            out = Path(td)
            write_ir(unit, out, bundle=False)
            for path in sorted(GOLDEN.glob("*.ir.json")):
                got = json.loads((out / path.name).read_text(encoding="utf-8"))
                want = json.loads(path.read_text(encoding="utf-8"))
                self.assertEqual(got, want, msg=path.name)

    def test_check_player(self) -> None:
        _, diags = compile_file(ENTRY, root=REPO)
        self.assertFalse(any(d.level == "error" for d in diags))


if __name__ == "__main__":
    unittest.main()
