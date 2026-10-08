#!/usr/bin/env python3
"""Diff two property *.schema.json files for breaking changes.

Exit 0 if compatible (only appends / identical).
Exit 1 if breaking (deleted index, kind/name/item_class change).

Usage:
  python3 scripts/diff_schema.py old/Player.schema.json new/Player.schema.json
  python3 scripts/diff_schema.py --dir-old baseline/schema --dir-new generated/schema
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any, Dict, List, Tuple


def load_schema(path: Path) -> Dict[str, Any]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if "fields" not in data or "class" not in data:
        raise ValueError(f"invalid schema: {path}")
    return data


def index_map(schema: Dict[str, Any]) -> Dict[int, Dict[str, Any]]:
    out: Dict[int, Dict[str, Any]] = {}
    for f in schema["fields"]:
        out[int(f["index"])] = f
    return out


def diff_one(old: Dict[str, Any], new: Dict[str, Any]) -> List[str]:
    errors: List[str] = []
    warnings: List[str] = []
    old_fields = index_map(old)
    new_fields = index_map(new)
    cls = old.get("class", "?")

    for idx, of in old_fields.items():
        nf = new_fields.get(idx)
        if nf is None:
            errors.append(f"[{cls}] removed index {idx} (was name={of.get('name')})")
            continue
        for key in ("wire_kind", "item_class"):
            if of.get(key, "") != nf.get(key, ""):
                errors.append(
                    f"[{cls}] index {idx} {key} changed: {of.get(key)!r} -> {nf.get(key)!r}"
                )
        if of.get("name") != nf.get("name"):
            warnings.append(
                f"[{cls}] index {idx} renamed: {of.get('name')!r} -> {nf.get('name')!r}"
            )

    old_max = max(old_fields) if old_fields else 0
    for idx, nf in sorted(new_fields.items()):
        if idx not in old_fields:
            if idx < old_max:
                errors.append(
                    f"[{cls}] new index {idx} inserted before old max {old_max} "
                    f"(append-only rule); name={nf.get('name')}"
                )
            else:
                warnings.append(f"[{cls}] appended index {idx} name={nf.get('name')}")

    for w in warnings:
        print(f"WARN: {w}", file=sys.stderr)
    return errors


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("old", nargs="?", help="old schema json")
    ap.add_argument("new", nargs="?", help="new schema json")
    ap.add_argument("--dir-old", type=Path, help="directory of old *.schema.json")
    ap.add_argument("--dir-new", type=Path, help="directory of new *.schema.json")
    args = ap.parse_args()

    pairs: List[Tuple[Path, Path]] = []
    if args.dir_old and args.dir_new:
        old_files = {p.name: p for p in args.dir_old.glob("*.schema.json")}
        new_files = {p.name: p for p in args.dir_new.glob("*.schema.json")}
        for name, op in sorted(old_files.items()):
            if name not in new_files:
                print(f"ERROR: missing schema in new dir: {name}", file=sys.stderr)
                return 1
            pairs.append((op, new_files[name]))
    elif args.old and args.new:
        pairs.append((Path(args.old), Path(args.new)))
    else:
        ap.error("provide old new files, or --dir-old and --dir-new")

    all_errors: List[str] = []
    for op, np in pairs:
        all_errors.extend(diff_one(load_schema(op), load_schema(np)))

    if all_errors:
        for e in all_errors:
            print(f"ERROR: {e}", file=sys.stderr)
        return 1
    print("OK: schema compatible")
    return 0


if __name__ == "__main__":
    sys.exit(main())
