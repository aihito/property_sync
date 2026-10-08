"""Semantic validation (dsl-design V1–V7) — extensible rule list."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable, List

from .models import (
    FIRST_CLASS_SCALARS,
    SCALAR_NAMES,
    ClassDef,
    CompilationUnit,
    FlagsDef,
)


@dataclass
class Diagnostic:
    level: str  # error | warning
    code: str
    message: str

    def __str__(self) -> str:
        return f"{self.level.upper()} [{self.code}] {self.message}"


RuleFn = Callable[[CompilationUnit], List[Diagnostic]]


def _implicit_reserved(cls: ClassDef) -> set[int]:
    if cls.kind == "bag_item":
        return {0}
    if cls.kind == "slot_item":
        return {0, 1}
    return set()


def _flag_names(flags: FlagsDef) -> set[str]:
    names = {b.name for b in flags.bits}
    names |= {a.name for a in flags.aliases}
    return names


def rule_unique_class_names(unit: CompilationUnit) -> List[Diagnostic]:
    # dict keys already unique; check namespace+name
    seen: dict[str, str] = {}
    out: List[Diagnostic] = []
    for cls in unit.classes.values():
        key = f"{cls.namespace}::{cls.name}"
        if key in seen:
            out.append(Diagnostic("error", "V7", f"duplicate class {key}"))
        seen[key] = cls.source_file
    return out


def rule_field_indexes(unit: CompilationUnit) -> List[Diagnostic]:
    out: List[Diagnostic] = []
    for cls in unit.classes.values():
        reserved = set(cls.reserved) | _implicit_reserved(cls)
        used: dict[int, str] = {}
        for f in cls.fields:
            if f.index < 0 or f.index > 254:
                out.append(
                    Diagnostic("error", "V1", f"{cls.name}.{f.name}: index {f.index} out of range")
                )
            if f.index in reserved and not f.deprecated:
                # declaring on reserved implicit slot is always wrong
                if f.index in _implicit_reserved(cls):
                    out.append(
                        Diagnostic(
                            "error",
                            "V1",
                            f"{cls.name}.{f.name}: index {f.index} reserved by {cls.kind}",
                        )
                    )
            if f.index in used:
                out.append(
                    Diagnostic(
                        "error",
                        "V1",
                        f"{cls.name}: index {f.index} used by both {used[f.index]} and {f.name}",
                    )
                )
            used[f.index] = f.name
            if f.index in cls.reserved:
                out.append(
                    Diagnostic(
                        "error",
                        "V5",
                        f"{cls.name}.{f.name}: index {f.index} is in reserved list",
                    )
                )
        for r in cls.reserved:
            if r in _implicit_reserved(cls):
                out.append(
                    Diagnostic(
                        "warning",
                        "V5",
                        f"{cls.name}: reserved {r} overlaps implicit {cls.kind} indexes",
                    )
                )
    return out


def rule_container_item_kinds(unit: CompilationUnit) -> List[Diagnostic]:
    out: List[Diagnostic] = []
    for cls in unit.classes.values():
        for f in cls.fields:
            t = f.type
            if t.kind == "bag":
                item = unit.classes.get(t.name or "")
                if not item:
                    out.append(Diagnostic("error", "V2", f"{cls.name}.{f.name}: unknown bag item {t.name}"))
                elif item.kind != "bag_item":
                    out.append(
                        Diagnostic("error", "V2", f"{cls.name}.{f.name}: bag<> requires bag_item, got {item.kind}")
                    )
            elif t.kind == "slots":
                item = unit.classes.get(t.name or "")
                if not item:
                    out.append(Diagnostic("error", "V2", f"{cls.name}.{f.name}: unknown slots item {t.name}"))
                elif item.kind != "slot_item":
                    out.append(
                        Diagnostic(
                            "error", "V2", f"{cls.name}.{f.name}: slots<> requires slot_item, got {item.kind}"
                        )
                    )
            elif t.kind == "vec":
                item = unit.classes.get(t.name or "")
                if not item:
                    out.append(Diagnostic("error", "V2", f"{cls.name}.{f.name}: unknown vec item {t.name}"))
                elif item.kind != "vec_item":
                    out.append(
                        Diagnostic("error", "V2", f"{cls.name}.{f.name}: vec<> requires vec_item, got {item.kind}")
                    )
            elif t.kind == "object":
                item = unit.classes.get(t.name or "")
                if not item:
                    out.append(Diagnostic("error", "V2", f"{cls.name}.{f.name}: unknown object {t.name}"))
                elif item.kind not in ("object", "entity"):
                    out.append(
                        Diagnostic(
                            "error",
                            "V2",
                            f"{cls.name}.{f.name}: object<> expects object/entity, got {item.kind}",
                        )
                    )
    return out


def rule_flags_resolve(unit: CompilationUnit) -> List[Diagnostic]:
    out: List[Diagnostic] = []
    for cls in unit.classes.values():
        if not cls.flags_ref:
            if any(f.flags for f in cls.fields):
                out.append(
                    Diagnostic("error", "V3", f"{cls.name}: fields have flags but no `using flags`")
                )
            continue
        fd = unit.flags.get(cls.flags_ref)
        if not fd:
            out.append(Diagnostic("error", "V3", f"{cls.name}: unknown flags {cls.flags_ref}"))
            continue
        known = _flag_names(fd)
        for f in cls.fields:
            for name in f.flags:
                if name not in known:
                    out.append(
                        Diagnostic("error", "V3", f"{cls.name}.{f.name}: unknown flag {name}")
                    )
    for fd in unit.flags.values():
        known = {b.name for b in fd.bits} | {a.name for a in fd.aliases}
        for a in fd.aliases:
            if a.expr == ["*"]:
                continue
            for part in a.expr:
                if part not in known or part == a.name:
                    out.append(
                        Diagnostic(
                            "error",
                            "V3",
                            f"flags {fd.name}: alias {a.name} references unknown {part}",
                        )
                    )
    return out


def rule_simple_container_elems(unit: CompilationUnit) -> List[Diagnostic]:
    out: List[Diagnostic] = []

    def ok_scalar(name: str | None) -> bool:
        return bool(name) and name in SCALAR_NAMES

    for cls in unit.classes.values():
        for f in cls.fields:
            t = f.type
            if t.kind == "array":
                if not ok_scalar(t.elem):
                    out.append(Diagnostic("error", "V4", f"{cls.name}.{f.name}: array elem must be scalar"))
                if t.size is None or t.size < 1:
                    out.append(Diagnostic("error", "V4", f"{cls.name}.{f.name}: array size must be >= 1"))
            elif t.kind == "list":
                if not ok_scalar(t.elem):
                    out.append(Diagnostic("error", "V4", f"{cls.name}.{f.name}: list elem must be scalar"))
            elif t.kind == "dict":
                if not ok_scalar(t.key) or not ok_scalar(t.value):
                    out.append(Diagnostic("error", "V4", f"{cls.name}.{f.name}: dict key/value must be scalar"))
                if t.key not in ("string", "int", "int32", "int64", "uint32", "uint64"):
                    out.append(
                        Diagnostic(
                            "warning",
                            "V4",
                            f"{cls.name}.{f.name}: unusual dict key type {t.key}",
                        )
                    )
    return out


def rule_version(unit: CompilationUnit) -> List[Diagnostic]:
    out: List[Diagnostic] = []
    for cls in unit.classes.values():
        if cls.schema_version < 1:
            out.append(Diagnostic("error", "V6", f"{cls.name}: version must be >= 1"))
    return out


def rule_first_class_scalars_warn(unit: CompilationUnit) -> List[Diagnostic]:
    """Warn when using late types outside first-class set."""
    out: List[Diagnostic] = []

    def check_name(where: str, name: str | None) -> None:
        if name and name in SCALAR_NAMES and name not in FIRST_CLASS_SCALARS:
            out.append(
                Diagnostic("warning", "T1", f"{where}: scalar {name} is late-tier (see dsl-types.md)")
            )

    for cls in unit.classes.values():
        if cls.key_type:
            check_name(f"{cls.name} key", cls.key_type)
        for f in cls.fields:
            t = f.type
            if t.kind == "scalar":
                check_name(f"{cls.name}.{f.name}", t.name)
            elif t.kind == "array":
                check_name(f"{cls.name}.{f.name}", t.elem)
            elif t.kind == "list":
                check_name(f"{cls.name}.{f.name}", t.elem)
            elif t.kind == "dict":
                check_name(f"{cls.name}.{f.name}", t.key)
                check_name(f"{cls.name}.{f.name}", t.value)
    return out


DEFAULT_RULES: List[RuleFn] = [
    rule_unique_class_names,
    rule_field_indexes,
    rule_container_item_kinds,
    rule_flags_resolve,
    rule_simple_container_elems,
    rule_version,
    rule_first_class_scalars_warn,
]


def validate(unit: CompilationUnit, rules: List[RuleFn] | None = None) -> List[Diagnostic]:
    diags: List[Diagnostic] = []
    for rule in rules or DEFAULT_RULES:
        diags.extend(rule(unit))
    return diags


def has_errors(diags: List[Diagnostic]) -> bool:
    return any(d.level == "error" for d in diags)
