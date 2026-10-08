"""Load .psync graph → CompilationUnit → IR JSON files."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Optional, Set

from .models import CompilationUnit
from .parser import ParseError, parse_path
from .validate import Diagnostic, has_errors, validate


class CompileError(Exception):
    def __init__(self, message: str, diagnostics: Optional[list[Diagnostic]] = None):
        super().__init__(message)
        self.diagnostics = diagnostics or []


def _relpath(path: Path, root: Path) -> str:
    try:
        return str(path.resolve().relative_to(root.resolve())).replace("\\", "/")
    except ValueError:
        return str(path.resolve()).replace("\\", "/")


def _resolve_import(from_file: Path, rel: str) -> Path:
    candidate = (from_file.parent / rel).resolve()
    if not candidate.is_file():
        raise CompileError(f"{from_file}: import not found: {rel}")
    return candidate


def load_unit(
    entry: Path,
    *,
    root: Optional[Path] = None,
    _visited: Optional[Set[Path]] = None,
) -> CompilationUnit:
    """Parse entry and transitive imports into one CompilationUnit.

    ``root`` is used to relativize ``entry`` / ``source_file`` in IR (defaults to entry's parent).
    """
    entry = entry.resolve()
    root = (root or entry.parent).resolve()
    visited = _visited if _visited is not None else set()
    unit = CompilationUnit(entry=_relpath(entry, root))

    def load_one(path: Path) -> None:
        path = path.resolve()
        if path in visited:
            return
        visited.add(path)

        try:
            imports, flags_list, classes = parse_path(path)
        except ParseError as e:
            raise CompileError(f"{path}: {e}") from e

        for rel in imports:
            load_one(_resolve_import(path, rel))

        for fd in flags_list:
            if fd.name in unit.flags:
                raise CompileError(f"duplicate flags {fd.name} (from {path}; already defined)")
            unit.flags[fd.name] = fd

        for cls in classes:
            cls.source_file = _relpath(path, root)
            if cls.name in unit.classes:
                other = unit.classes[cls.name]
                raise CompileError(
                    f"duplicate class {cls.name} in {path} "
                    f"(already from {other.source_file})"
                )
            unit.classes[cls.name] = cls

    load_one(entry)
    return unit


def compile_file(
    entry: Path,
    *,
    strict: bool = True,
    root: Optional[Path] = None,
) -> tuple[CompilationUnit, list[Diagnostic]]:
    unit = load_unit(entry, root=root)
    diags = validate(unit)
    if strict and has_errors(diags):
        raise CompileError("validation failed", diagnostics=diags)
    return unit, diags


def write_ir(unit: CompilationUnit, out_dir: Path, *, bundle: bool = True) -> list[Path]:
    """Write per-class / per-flags IR JSON; optionally a bundle."""
    out_dir.mkdir(parents=True, exist_ok=True)
    written: list[Path] = []

    for name, fd in sorted(unit.flags.items()):
        p = out_dir / f"{name}.ir.json"
        p.write_text(json.dumps(fd.to_ir(), indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        written.append(p)

    for name, cls in sorted(unit.classes.items()):
        p = out_dir / f"{name}.ir.json"
        p.write_text(json.dumps(cls.to_ir(), indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        written.append(p)

    if bundle:
        p = out_dir / "_bundle.ir.json"
        p.write_text(
            json.dumps(unit.to_bundle_ir(), indent=2, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )
        written.append(p)

    return written
