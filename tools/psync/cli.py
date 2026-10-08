"""CLI for psync."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from . import __version__
from .compile import CompileError, compile_file, write_ir
from .emit import write_emitted


def _print_diags(diags) -> None:
    for d in diags:
        print(d, file=sys.stderr)


def _root_arg(args: argparse.Namespace) -> Path | None:
    return Path(args.root) if getattr(args, "root", None) else None


def cmd_compile(args: argparse.Namespace) -> int:
    entry = Path(args.entry)
    if not entry.is_file():
        print(f"error: entry not found: {entry}", file=sys.stderr)
        return 2
    try:
        unit, diags = compile_file(entry, strict=not args.allow_errors, root=_root_arg(args))
    except CompileError as e:
        print(f"error: {e}", file=sys.stderr)
        _print_diags(e.diagnostics)
        return 1
    _print_diags(diags)
    out = Path(args.output)
    written = write_ir(unit, out, bundle=not args.no_bundle)
    if args.verbose:
        for p in written:
            print(p)
    else:
        print(f"wrote {len(written)} file(s) → {out}")
    return 0


def cmd_check(args: argparse.Namespace) -> int:
    entry = Path(args.entry)
    if not entry.is_file():
        print(f"error: entry not found: {entry}", file=sys.stderr)
        return 2
    try:
        _, diags = compile_file(entry, strict=True, root=_root_arg(args))
    except CompileError as e:
        print(f"error: {e}", file=sys.stderr)
        _print_diags(e.diagnostics)
        return 1
    warnings = [d for d in diags if d.level == "warning"]
    _print_diags(warnings)
    print(f"ok: {entry}")
    return 0


def cmd_emit(args: argparse.Namespace) -> int:
    entry = Path(args.entry)
    if not entry.is_file():
        print(f"error: entry not found: {entry}", file=sys.stderr)
        return 2
    try:
        unit, diags = compile_file(entry, strict=not args.allow_errors, root=_root_arg(args))
    except CompileError as e:
        print(f"error: {e}", file=sys.stderr)
        _print_diags(e.diagnostics)
        return 1
    _print_diags(diags)
    out = Path(args.output)
    written = write_emitted(
        unit,
        out,
        legacy_wire=not args.native_wire,
        copy_runtime=not args.no_runtime,
    )
    if args.verbose:
        for p in written:
            print(p)
    else:
        print(f"emitted {len(written)} file(s) → {out}/{{schema,lua,proto}}")
    return 0


def cmd_dump(args: argparse.Namespace) -> int:
    entry = Path(args.entry)
    if not entry.is_file():
        print(f"error: entry not found: {entry}", file=sys.stderr)
        return 2
    try:
        unit, diags = compile_file(entry, strict=not args.allow_errors, root=_root_arg(args))
    except CompileError as e:
        print(f"error: {e}", file=sys.stderr)
        _print_diags(e.diagnostics)
        return 1
    _print_diags(diags)
    print(json.dumps(unit.to_bundle_ir(), indent=2, ensure_ascii=False))
    return 0


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog="psync", description="Compile .psync DSL to IR JSON")
    p.add_argument("--version", action="version", version=f"psync {__version__}")
    sub = p.add_subparsers(dest="command", required=True)

    def add_root(sp: argparse.ArgumentParser) -> None:
        sp.add_argument(
            "--root",
            help="path root for relativizing entry/source_file in IR (default: entry parent)",
        )

    c = sub.add_parser("compile", help="compile entry .psync → IR JSON files")
    c.add_argument("entry", help="entry .psync path")
    c.add_argument("-o", "--output", required=True, help="output directory")
    c.add_argument("--no-bundle", action="store_true", help="skip _bundle.ir.json")
    c.add_argument("--allow-errors", action="store_true", help="emit IR even with validation errors")
    c.add_argument("-v", "--verbose", action="store_true")
    add_root(c)
    c.set_defaults(func=cmd_compile)

    k = sub.add_parser("check", help="parse + validate only")
    k.add_argument("entry", help="entry .psync path")
    add_root(k)
    k.set_defaults(func=cmd_check)

    d = sub.add_parser("dump", help="print bundle IR JSON to stdout")
    d.add_argument("entry", help="entry .psync path")
    d.add_argument("--allow-errors", action="store_true")
    add_root(d)
    d.set_defaults(func=cmd_dump)

    e = sub.add_parser("emit", help="compile + emit schema/lua/proto (S2 dual-track)")
    e.add_argument("entry", help="entry .psync path")
    e.add_argument("-o", "--output", required=True, help="output root (writes schema/, lua/, proto/)")
    e.add_argument(
        "--native-wire",
        action="store_true",
        help="emit DSL wire names list/dict (default: map to vector/map for Meta compat)",
    )
    e.add_argument("--no-runtime", action="store_true", help="do not copy property_runtime.lua / json.lua")
    e.add_argument("--allow-errors", action="store_true")
    e.add_argument("-v", "--verbose", action="store_true")
    add_root(e)
    e.set_defaults(func=cmd_emit)

    return p


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return args.func(args)
