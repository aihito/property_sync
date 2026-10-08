"""IR data models — language-neutral, JSON-serializable."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal, Optional, Union

ClassKind = Literal["entity", "bag_item", "slot_item", "vec_item", "object"]
WireKind = Literal[
    "bool",
    "number",
    "string",
    "array",
    "list",
    "dict",
    "bag",
    "slots",
    "vec",
    "object",
    "other",
]

SCALAR_NAMES = frozenset(
    {
        "bool",
        "int",
        "int8",
        "int16",
        "int32",
        "int64",
        "uint8",
        "uint16",
        "uint32",
        "uint64",
        "float",
        "double",
        "string",
    }
)

FIRST_CLASS_SCALARS = frozenset(
    {"bool", "int", "int32", "int64", "uint32", "uint64", "float", "double", "string"}
)


@dataclass
class FlagBit:
    name: str
    bit: int


@dataclass
class FlagAlias:
    name: str
    # list of bit/alias names, or ["*"] for mask_all
    expr: list[str]


@dataclass
class FlagsDef:
    name: str
    bits: list[FlagBit] = field(default_factory=list)
    aliases: list[FlagAlias] = field(default_factory=list)

    def to_ir(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "bits": [{"name": b.name, "bit": b.bit} for b in self.bits],
            "aliases": [{"name": a.name, "expr": a.expr} for a in self.aliases],
        }


@dataclass
class TypeRef:
    """Normalized type node."""

    kind: str  # scalar|array|list|dict|bag|slots|vec|object
    name: Optional[str] = None  # scalar name or item class
    elem: Optional[str] = None
    size: Optional[int] = None
    key: Optional[str] = None
    value: Optional[str] = None

    def wire_kind(self) -> WireKind:
        if self.kind == "scalar":
            assert self.name
            if self.name == "bool":
                return "bool"
            if self.name == "string":
                return "string"
            return "number"
        if self.kind == "array":
            return "array"
        if self.kind == "list":
            return "list"
        if self.kind == "dict":
            return "dict"
        if self.kind == "bag":
            return "bag"
        if self.kind == "slots":
            return "slots"
        if self.kind == "vec":
            return "vec"
        if self.kind == "object":
            return "object"
        return "other"

    def to_ir(self) -> dict[str, Any]:
        out: dict[str, Any] = {"kind": self.kind}
        if self.kind == "scalar":
            out["name"] = self.name
        elif self.kind == "array":
            out["elem"] = self.elem
            out["size"] = self.size
        elif self.kind == "list":
            out["elem"] = self.elem
        elif self.kind == "dict":
            out["key"] = self.key
            out["value"] = self.value
        elif self.kind in ("bag", "slots", "vec", "object"):
            out["item"] = self.name
        return out


@dataclass
class FieldDef:
    index: int
    name: str
    type: TypeRef
    flags: list[str] = field(default_factory=list)
    default: Any = None
    deprecated: bool = False
    deprecated_reason: Optional[str] = None

    def to_ir(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "index": self.index,
            "name": self.name,
            "type": self.type.to_ir(),
            "wire_kind": self.type.wire_kind(),
            "flags": list(self.flags),
            "deprecated": self.deprecated,
        }
        if self.default is not None:
            d["default"] = self.default
        if self.deprecated_reason:
            d["deprecated_reason"] = self.deprecated_reason
        # convenience for emitters
        if self.type.kind in ("bag", "slots", "vec", "object"):
            d["item_class"] = self.type.name
        else:
            d["item_class"] = ""
        return d


@dataclass
class ClassDef:
    kind: ClassKind
    name: str
    namespace: str = ""
    schema_version: int = 1
    key_type: Optional[str] = None  # bag_item / slot_item
    fields: list[FieldDef] = field(default_factory=list)
    reserved: list[int] = field(default_factory=list)
    flags_ref: Optional[str] = None
    source_file: str = ""

    def to_ir(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "kind": self.kind,
            "namespace": self.namespace,
            "schema_version": self.schema_version,
            "key_type": self.key_type,
            "flags_ref": self.flags_ref,
            "fields": [f.to_ir() for f in sorted(self.fields, key=lambda x: x.index)],
            "reserved": sorted(self.reserved),
            "source_file": self.source_file,
        }


@dataclass
class CompilationUnit:
    entry: str
    flags: dict[str, FlagsDef] = field(default_factory=dict)
    classes: dict[str, ClassDef] = field(default_factory=dict)

    def to_bundle_ir(self) -> dict[str, Any]:
        return {
            "entry": self.entry,
            "flags": {k: v.to_ir() for k, v in sorted(self.flags.items())},
            "classes": {k: v.to_ir() for k, v in sorted(self.classes.items())},
        }


def dump_ir(obj: Union[FlagsDef, ClassDef, CompilationUnit]) -> dict[str, Any]:
    if isinstance(obj, FlagsDef):
        return obj.to_ir()
    if isinstance(obj, ClassDef):
        return obj.to_ir()
    return obj.to_bundle_ir()
