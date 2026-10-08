"""IR → schema / lua / proto (S2 dual-track; matches Meta mustache semantics).

Wire transition (S4 will drop this): list→vector, dict→map in emitted artifacts.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Optional

from .models import ClassDef, CompilationUnit, FieldDef, FlagsDef, TypeRef

# Legacy emitter wire names expected by property_runtime.lua today
_WIRE_LEGACY = {"list": "vector", "dict": "map"}

_CPP_SCALAR = {
    "bool": "bool",
    "int": "int",
    "int8": "std::int8_t",
    "int16": "std::int16_t",
    "int32": "std::int32_t",
    "int64": "std::int64_t",
    "uint8": "std::uint8_t",
    "uint16": "std::uint16_t",
    "uint32": "std::uint32_t",
    "uint64": "std::uint64_t",
    "float": "float",
    "double": "double",
    "string": "std::string",
}

_PROTO_SCALAR = {
    "bool": "bool",
    "int": "int64",
    "int8": "int64",
    "int16": "int64",
    "int32": "int64",
    "int64": "int64",
    "uint8": "uint64",
    "uint16": "uint64",
    "uint32": "uint64",
    "uint64": "uint64",
    "float": "float",
    "double": "float",
    "string": "string",
}


def ns_cpp(dotted: str) -> str:
    return dotted.replace(".", "::") if dotted else ""


def qual(ns: str, name: str) -> str:
    cpp_ns = ns_cpp(ns)
    return f"{cpp_ns}::{name}" if cpp_ns else name


def map_wire(wire: str, *, legacy: bool) -> str:
    """When legacy=True, map list→vector / dict→map for Meta-era runtime artifacts."""
    if legacy:
        return _WIRE_LEGACY.get(wire, wire)
    return wire


def cpp_type_of(t: TypeRef, ns: str) -> str:
    if t.kind == "scalar":
        assert t.name
        return _CPP_SCALAR.get(t.name, t.name)
    if t.kind == "array":
        elem = _CPP_SCALAR.get(t.elem or "", t.elem or "?")
        return f"std::array<{elem}, {t.size}>"
    if t.kind == "list":
        elem = _CPP_SCALAR.get(t.elem or "", t.elem or "?")
        return f"std::vector<{elem}>"
    if t.kind == "dict":
        k = _CPP_SCALAR.get(t.key or "", t.key or "?")
        v = _CPP_SCALAR.get(t.value or "", t.value or "?")
        return f"std::unordered_map<{k}, {v}>"
    if t.kind in ("bag", "slots", "vec", "object"):
        item = qual(ns, t.name or "?")
        prefix = {
            "bag": "spiritsaway::property::property_bag",
            "slots": "spiritsaway::property::property_slots",
            "vec": "spiritsaway::property::property_vec",
            "object": "",
        }[t.kind]
        return item if t.kind == "object" else f"{prefix}<{item}>"
    return "?"


def proto_type_of(t: TypeRef) -> str:
    if t.kind == "scalar":
        assert t.name
        return _PROTO_SCALAR.get(t.name, "bytes")
    if t.kind == "array":
        elem = _PROTO_SCALAR.get(t.elem or "float", "float")
        return f"repeated {elem}"
    if t.kind == "list":
        elem = _PROTO_SCALAR.get(t.elem or "string", "string")
        return f"repeated {elem}"
    if t.kind == "dict":
        k = "string" if (t.key or "") == "string" else _PROTO_SCALAR.get(t.key or "string", "string")
        # Meta map_proto_type mostly assumes string keys
        if (t.key or "string") != "string":
            k = _PROTO_SCALAR.get(t.key or "string", "string")
        v = _PROTO_SCALAR.get(t.value or "string", "string")
        return f"map<{k}, {v}>"
    if t.kind == "bag":
        return f"repeated {t.name}Snapshot"
    if t.kind == "slots":
        return f"{t.name}SlotsSnapshot"
    if t.kind == "vec":
        return f"repeated {t.name}Snapshot"
    if t.kind == "object":
        return f"{t.name}Snapshot"
    return "bytes"


def item_class_of(t: TypeRef, ns: str) -> str:
    if t.kind in ("bag", "slots", "vec", "object") and t.name:
        return qual(ns, t.name)
    return ""


@dataclass
class EmitField:
    index: int
    name: str
    wire_kind: str  # legacy-mapped
    cpp_type: str
    proto_type: str
    item_class: str  # qualified or ""
    item_short: str
    flags: list[str]
    has_item_sync: bool


@dataclass
class EmitClass:
    name: str
    namespace: str  # dotted
    schema_version: int
    kind: str
    fields: list[EmitField]
    proto_imports: list[str]

    @property
    def cpp_namespace(self) -> str:
        return ns_cpp(self.namespace)

    @property
    def qualified_name(self) -> str:
        return qual(self.namespace, self.name)

    @property
    def is_bag_item(self) -> bool:
        return self.kind == "bag_item"

    @property
    def is_slot_item(self) -> bool:
        return self.kind == "slot_item"

    @property
    def is_vec_item(self) -> bool:
        return self.kind == "vec_item"

    @property
    def is_property_item(self) -> bool:
        return self.kind in ("bag_item", "slot_item", "vec_item")

    @property
    def has_bag_id(self) -> bool:
        return self.kind in ("bag_item", "slot_item")


def build_emit_class(cls: ClassDef, *, legacy_wire: bool = True) -> EmitClass:
    fields: list[EmitField] = []
    imports: list[str] = []
    seen: set[str] = set()

    for f in sorted(cls.fields, key=lambda x: x.index):
        wire = map_wire(f.type.wire_kind(), legacy=legacy_wire)
        item_q = item_class_of(f.type, cls.namespace)
        item_short = f.type.name or "" if f.type.kind in ("bag", "slots", "vec", "object") else ""
        has_sync = f.type.kind in ("bag", "slots", "vec")
        if has_sync and item_short:
            imp = f"{item_short}.proto"
            if imp not in seen:
                seen.add(imp)
                imports.append(imp)
        fields.append(
            EmitField(
                index=f.index,
                name=f.name,
                wire_kind=wire,
                cpp_type=cpp_type_of(f.type, cls.namespace),
                proto_type=proto_type_of(f.type),
                item_class=item_q,
                item_short=item_short,
                flags=list(f.flags),
                has_item_sync=has_sync,
            )
        )

    return EmitClass(
        name=cls.name,
        namespace=cls.namespace,
        schema_version=cls.schema_version,
        kind=cls.kind,
        fields=fields,
        proto_imports=imports,
    )


def emit_schema_json(ec: EmitClass) -> dict[str, Any]:
    return {
        "class": ec.name,
        "qualified_name": ec.qualified_name,
        "namespace": ec.cpp_namespace,
        "schema_version": ec.schema_version,
        "fields": [
            {
                "index": f.index,
                "name": f.name,
                "cpp_type": f.cpp_type,
                "wire_kind": f.wire_kind,
                "item_class": f.item_class,
                "flags": list(f.flags),
            }
            for f in ec.fields
        ],
    }


def emit_flags_lua(flags: Optional[FlagsDef]) -> str:
    if not flags:
        return "nil"
    bit_parts = ", ".join(f'{b.name} = {b.bit}' for b in flags.bits)
    alias_parts = []
    for a in flags.aliases:
        inner = ", ".join(f'"{x}"' for x in a.expr)
        alias_parts.append(f"{a.name} = {{ {inner} }}")
    aliases = ", ".join(alias_parts)
    return "{ bits = { " + bit_parts + " }, aliases = { " + aliases + " } }"


def emit_lua_meta(ec: EmitClass, flags: Optional[FlagsDef] = None) -> str:
    _ = flags  # FLAGS live on *_record.lua
    lines: list[str] = [
        "-- Auto-generated by psync emit — do not edit.",
        "-- Property class metadata (fields / INDEX / wire_kind); Replay via property_runtime.",
        'local Runtime = require("property_runtime")',
    ]
    for f in ec.fields:
        if f.has_item_sync:
            lines.append(f'local {f.item_short}Meta = require("{f.item_short}_meta")')
    lines += [
        "",
        "local M = {",
        f"  SCHEMA_VERSION = {ec.schema_version},",
        f'  CLASS_NAME = "{ec.name}",',
        f"  has_slot = {'true' if ec.is_slot_item else 'false'},",
        f"  has_bag_id = {'true' if ec.has_bag_id else 'false'},",
        "  fields = {",
    ]
    for f in ec.fields:
        flag_lit = ", ".join(f'"{x}"' for x in f.flags)
        extra = f", item_meta = {f.item_short}Meta.META" if f.has_item_sync else ""
        lines.append(
            f'    {{ index = {f.index}, name = "{f.name}", wire_kind = "{f.wire_kind}", '
            f"flags = {{ {flag_lit} }}{extra} }},"
        )
    index_parts = ", ".join(f"{f.name} = {f.index}" for f in ec.fields)
    lines += [
        "  },",
        f"  INDEX = {{ {index_parts} }},",
        "}",
        "",
        "return Runtime.attach_meta(M)",
        "",
    ]
    return "\n".join(lines)


def emit_lua_record(ec: EmitClass, flags: Optional[FlagsDef] = None) -> str:
    """Minimal binder — call sites use Record methods: rec:set / bag_insert / …"""
    return "\n".join(
        [
            "-- Auto-generated by psync emit — do not edit.",
            '-- Usage: local rec = require("Player_record").new(); rec.hp = 80; rec.tags:push("vip")',
            '-- Or:    local data = Meta.new_default(); require("Player_record").open(data)',
            'local Record = require("property_record")',
            f'local Meta = require("{ec.name}_meta")',
            "",
            "local M = {}",
            f"M.FLAGS = {emit_flags_lua(flags)}",
            "M.Meta = Meta",
            "",
            "function M.new(opts)",
            "  opts = opts or {}",
            "  opts.flags = opts.flags or M.FLAGS",
            '  opts.need_flag_names = opts.need_flag_names or { "sync_clients" }',
            "  return Record.bind(Meta, opts)",
            "end",
            "",
            "--- Open over an existing 源表.",
            "function M.open(obj, opts)",
            "  opts = opts or {}",
            "  opts.obj = obj",
            "  return M.new(opts)",
            "end",
            "",
            "return M",
            "",
        ]
    )


def emit_proto(ec: EmitClass) -> str:
    lines = [
        'syntax = "proto3";',
        "// Auto-generated by psync emit — do not edit.",
        "// Field numbers: business = property index + 1 (proto forbids 0); schema_version = 1000.",
        "// Compatibility: only append fields; never reuse numbers. See docs/compatibility.md.",
        "",
        "package property_sync.generated;",
        "",
    ]
    for imp in ec.proto_imports:
        lines.append(f'import "{imp}";')
    if ec.proto_imports:
        lines.append("")
    if ec.is_property_item:
        lines.append("// Item / bag / slot / vec element snapshot")
    lines.append(f"message {ec.name}Snapshot {{")
    lines.append("  uint32 schema_version = 1000;")
    if ec.is_bag_item:
        lines.append("  int64 id = 1; // index=0 bag key")
    if ec.is_slot_item:
        lines.append("  int64 id = 1; // index=0")
        lines.append("  uint32 slot = 2; // index=1")
    for f in ec.fields:
        num = f.index + 1
        lines.append(f"  {f.proto_type} {f.name} = {num}; // index={f.index} kind={f.wire_kind}")
    lines.append("}")
    lines.append("")
    if ec.is_slot_item:
        lines += [
            f"// Mirrors C++ property_slots encode: {{ sz, data }}",
            f"message {ec.name}SlotsSnapshot {{",
            "  uint32 sz = 1;",
            f"  repeated {ec.name}Snapshot data = 2;",
            "}",
            "",
        ]
    return "\n".join(lines)


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def copy_lua_runtime(lua_dir: Path, runtime_src: Path) -> list[Path]:
    """Copy hand-maintained runtime next to generated *_meta.lua."""
    import shutil

    written: list[Path] = []
    lua_dir.mkdir(parents=True, exist_ok=True)
    for name in ("property_runtime.lua", "property_record.lua", "json.lua"):
        src = runtime_src / name
        if not src.is_file():
            continue
        dst = lua_dir / name
        shutil.copy2(src, dst)
        written.append(dst)
    cmd_src = _repo_root() / "meta" / "mustache" / "property_cmd_lua.mustache"
    if cmd_src.is_file():
        dst = lua_dir / "property_cmd.lua"
        dst.write_text(cmd_src.read_text(encoding="utf-8"), encoding="utf-8")
        written.append(dst)
    return written


def write_emitted(
    unit: CompilationUnit,
    out_dir: Path,
    *,
    legacy_wire: bool = True,
    copy_runtime: bool = True,
    runtime_dir: Optional[Path] = None,
) -> list[Path]:
    """Write schema/, lua/, proto/ under out_dir."""
    schema_dir = out_dir / "schema"
    lua_dir = out_dir / "lua"
    proto_dir = out_dir / "proto"
    for d in (schema_dir, lua_dir, proto_dir):
        d.mkdir(parents=True, exist_ok=True)

    import json

    written: list[Path] = []
    flags_by_name = unit.flags

    for cls in sorted(unit.classes.values(), key=lambda c: c.name):
        ec = build_emit_class(cls, legacy_wire=legacy_wire)
        fd = flags_by_name.get(cls.flags_ref) if cls.flags_ref else None

        sp = schema_dir / f"{cls.name}.schema.json"
        sp.write_text(json.dumps(emit_schema_json(ec), indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        written.append(sp)

        lp = lua_dir / f"{cls.name}_meta.lua"
        lp.write_text(emit_lua_meta(ec), encoding="utf-8")
        written.append(lp)

        rp = lua_dir / f"{cls.name}_record.lua"
        rp.write_text(emit_lua_record(ec, flags=fd), encoding="utf-8")
        written.append(rp)

        pp = proto_dir / f"{cls.name}.proto"
        pp.write_text(emit_proto(ec), encoding="utf-8")
        written.append(pp)

    if copy_runtime:
        root = runtime_dir or (_repo_root() / "meta" / "lua_runtime")
        written.extend(copy_lua_runtime(lua_dir, root))

    return written


# --- semantic compare helpers (Meta vs IR path) ---

def normalize_cpp_type(s: str) -> str:
    s = s.replace("std::__cxx11::basic_string<char>", "std::string")
    s = s.replace("std::array<float,>", "std::array<float,")  # Meta drops size — partial
    # collapse whitespace
    return "".join(s.split())


def schema_semantic(doc: dict[str, Any]) -> dict[str, Any]:
    fields = []
    for f in doc.get("fields", []):
        fields.append(
            {
                "index": f["index"],
                "name": f["name"],
                "wire_kind": f["wire_kind"],
                "item_class": f.get("item_class") or "",
                "flags": list(f.get("flags") or []),
            }
        )
    return {
        "class": doc["class"],
        "qualified_name": doc["qualified_name"],
        "namespace": doc["namespace"],
        "schema_version": doc["schema_version"],
        "fields": fields,
    }


def lua_semantic(text: str) -> str:
    """Strip comments/blank lines; keep structural lines for compare."""
    out = []
    for line in text.splitlines():
        s = line.strip()
        if not s or s.startswith("--"):
            continue
        out.append(s)
    return "\n".join(out)


def proto_semantic(text: str) -> str:
    out = []
    for line in text.splitlines():
        s = line.strip()
        if not s or s.startswith("//"):
            continue
        out.append(s)
    return "\n".join(out)
