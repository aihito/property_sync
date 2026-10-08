"""Recursive-descent parser: tokens → AST fragments (models)."""

from __future__ import annotations

from pathlib import Path
from typing import Any, List, Optional, Tuple  # Any used in defaults

from .lexer import LexerError, Token, tokenize
from .models import (
    SCALAR_NAMES,
    ClassDef,
    ClassKind,
    FieldDef,
    FlagAlias,
    FlagBit,
    FlagsDef,
    TypeRef,
)


class ParseError(Exception):
    def __init__(self, message: str, token: Optional[Token] = None):
        if token:
            super().__init__(f"{token.line}:{token.col}: {message}")
            self.line = token.line
            self.col = token.col
        else:
            super().__init__(message)
            self.line = 0
            self.col = 0


class Parser:
    def __init__(self, tokens: List[Token], source_file: str = ""):
        self.tokens = tokens
        self.pos = 0
        self.source_file = source_file
        self.namespace = ""
        self.flags_ref: Optional[str] = None
        self.imports: List[str] = []
        self.flags: List[FlagsDef] = []
        self.classes: List[ClassDef] = []

    # --- token helpers ---

    def cur(self) -> Token:
        return self.tokens[self.pos]

    def check(self, *kinds: str) -> bool:
        return self.cur().kind in kinds

    def advance(self) -> Token:
        tok = self.cur()
        if tok.kind != "EOF":
            self.pos += 1
        return tok

    def expect(self, *kinds: str) -> Token:
        if not self.check(*kinds):
            raise ParseError(f"expected {kinds}, got {self.cur().kind} ({self.cur().value!r})", self.cur())
        return self.advance()

    def match(self, *kinds: str) -> Optional[Token]:
        if self.check(*kinds):
            return self.advance()
        return None

    # --- top level ---

    def parse_file(self) -> Tuple[List[str], List[FlagsDef], List[ClassDef]]:
        while not self.check("EOF"):
            if self.check("IMPORT"):
                self._parse_import()
            elif self.check("FLAGS"):
                self.flags.append(self._parse_flags())
            elif self.check("NAMESPACE"):
                self._parse_namespace()
            elif self.check("USING"):
                self._parse_using()
            elif self.check("ENTITY", "BAG_ITEM", "SLOT_ITEM", "VEC_ITEM", "OBJECT"):
                self.classes.append(self._parse_class())
            else:
                raise ParseError(f"unexpected token {self.cur().kind}", self.cur())
        return self.imports, self.flags, self.classes

    def _parse_import(self) -> None:
        self.expect("IMPORT")
        path = self.expect("STRING").value
        self.imports.append(path)

    def _parse_namespace(self) -> None:
        self.expect("NAMESPACE")
        parts = [self.expect("IDENT").value]
        while self.match("DOT"):
            parts.append(self.expect("IDENT").value)
        self.namespace = ".".join(parts)

    def _parse_using(self) -> None:
        self.expect("USING")
        self.expect("FLAGS")
        self.flags_ref = self.expect("IDENT").value

    def _parse_flags(self) -> FlagsDef:
        self.expect("FLAGS")
        name = self.expect("IDENT").value
        self.expect("LBRACE")
        bits: List[FlagBit] = []
        aliases: List[FlagAlias] = []
        while not self.check("RBRACE"):
            if self.match("BIT"):
                bname = self.expect("IDENT").value
                self.expect("EQ")
                num = self.expect("NUMBER").value
                bits.append(FlagBit(bname, int(num)))
            elif self.match("ALIAS"):
                aname = self.expect("IDENT").value
                self.expect("EQ")
                expr = self._parse_alias_expr()
                aliases.append(FlagAlias(aname, expr))
            else:
                raise ParseError("expected bit or alias", self.cur())
        self.expect("RBRACE")
        return FlagsDef(name=name, bits=bits, aliases=aliases)

    def _parse_alias_expr(self) -> List[str]:
        if self.match("STAR"):
            return ["*"]
        names = [self.expect("IDENT").value]
        while self.match("PIPE"):
            names.append(self.expect("IDENT").value)
        return names

    def _parse_class(self) -> ClassDef:
        kind_tok = self.expect("ENTITY", "BAG_ITEM", "SLOT_ITEM", "VEC_ITEM", "OBJECT")
        kind_map = {
            "ENTITY": "entity",
            "BAG_ITEM": "bag_item",
            "SLOT_ITEM": "slot_item",
            "VEC_ITEM": "vec_item",
            "OBJECT": "object",
        }
        kind: ClassKind = kind_map[kind_tok.kind]  # type: ignore
        name = self.expect("IDENT").value
        self.expect("LBRACE")

        version = 1
        key_type: Optional[str] = None
        fields: List[FieldDef] = []
        reserved: List[int] = []

        while not self.check("RBRACE"):
            if self.match("VERSION"):
                version = int(self.expect("NUMBER").value)
            elif self.match("KEY"):
                key_type = self.expect("IDENT").value
                if key_type not in SCALAR_NAMES:
                    raise ParseError(f"invalid key type {key_type}", self.cur())
            elif self.match("RESERVED"):
                reserved.extend(self._parse_reserved_list())
            elif self.check("NUMBER"):
                fields.append(self._parse_field())
            else:
                raise ParseError("expected version, key, reserved, or field", self.cur())

        self.expect("RBRACE")

        if kind in ("bag_item", "slot_item") and not key_type:
            raise ParseError(f"{kind} {name} requires key <type>", kind_tok)

        return ClassDef(
            kind=kind,
            name=name,
            namespace=self.namespace,
            schema_version=version,
            key_type=key_type,
            fields=fields,
            reserved=reserved,
            flags_ref=self.flags_ref,
            source_file=self.source_file,
        )

    def _parse_reserved_list(self) -> List[int]:
        indexes: List[int] = []
        indexes.extend(self._parse_reserved_item())
        while self.match("COMMA"):
            indexes.extend(self._parse_reserved_item())
        return indexes

    def _parse_reserved_item(self) -> List[int]:
        start = int(self.expect("NUMBER").value)
        if self.match("TO"):
            end = int(self.expect("NUMBER").value)
            if end < start:
                raise ParseError(f"reserved range {start} to {end} invalid", self.cur())
            return list(range(start, end + 1))
        return [start]

    def _parse_field(self) -> FieldDef:
        index = int(self.expect("NUMBER").value)
        self.expect("COLON")
        name = self.expect("IDENT").value
        typ = self._parse_type()
        default: Any = None
        if self.match("EQ"):
            default = self._parse_default(typ)
        flags: List[str] = []
        if self.match("LBRACK"):
            if not self.check("RBRACK"):
                flags.append(self.expect("IDENT").value)
                while self.match("COMMA"):
                    flags.append(self.expect("IDENT").value)
            self.expect("RBRACK")
        deprecated = False
        reason: Optional[str] = None
        if self.match("DEPRECATED"):
            deprecated = True
            if self.check("STRING"):
                reason = self.advance().value
        return FieldDef(
            index=index,
            name=name,
            type=typ,
            flags=flags,
            default=default,
            deprecated=deprecated,
            deprecated_reason=reason,
        )

    def _parse_type(self) -> TypeRef:
        # map<...> alias → dict
        # array|list|dict|bag|slots|vec|object <...>
        # or scalar ident
        name = self.expect("IDENT").value

        if name == "map":
            name = "dict"

        if name in ("array", "list", "dict", "bag", "slots", "vec", "object"):
            self.expect("LT")
            if name == "array":
                elem = self.expect("IDENT").value
                self.expect("COMMA")
                size = int(self.expect("NUMBER").value)
                self.expect("GT")
                return TypeRef(kind="array", elem=elem, size=size)
            if name == "list":
                elem = self.expect("IDENT").value
                self.expect("GT")
                return TypeRef(kind="list", elem=elem)
            if name == "dict":
                key = self.expect("IDENT").value
                self.expect("COMMA")
                val = self.expect("IDENT").value
                self.expect("GT")
                return TypeRef(kind="dict", key=key, value=val)
            # bag / slots / vec / object
            item = self.expect("IDENT").value
            self.expect("GT")
            return TypeRef(kind=name, name=item)

        if name not in SCALAR_NAMES:
            raise ParseError(f"unknown type {name}", self.cur())
        return TypeRef(kind="scalar", name=name)

    def _parse_default(self, typ: TypeRef) -> Any:
        if self.check("TRUE"):
            self.advance()
            return True
        if self.check("FALSE"):
            self.advance()
            return False
        if self.check("STRING"):
            return self.advance().value
        if self.check("NUMBER"):
            text = self.advance().value
            if "." in text:
                return float(text)
            return int(text)
        if self.match("LBRACE"):
            vals: List[Any] = []
            if not self.check("RBRACE"):
                vals.append(self._parse_default_scalar())
                while self.match("COMMA"):
                    vals.append(self._parse_default_scalar())
            self.expect("RBRACE")
            if typ.kind == "array" and typ.size is not None and len(vals) != typ.size:
                raise ParseError(f"array default length {len(vals)} != {typ.size}", self.cur())
            return vals
        raise ParseError("invalid default", self.cur())

    def _parse_default_scalar(self) -> Any:
        if self.check("TRUE"):
            self.advance()
            return True
        if self.check("FALSE"):
            self.advance()
            return False
        if self.check("STRING"):
            return self.advance().value
        if self.check("NUMBER"):
            text = self.advance().value
            return float(text) if "." in text else int(text)
        raise ParseError("expected scalar in default list", self.cur())


def parse_source(src: str, source_file: str = "") -> Tuple[List[str], List[FlagsDef], List[ClassDef]]:
    try:
        tokens = tokenize(src, source_file)
    except LexerError as e:
        raise ParseError(str(e)) from e
    return Parser(tokens, source_file=source_file).parse_file()


def parse_path(path: Path) -> Tuple[List[str], List[FlagsDef], List[ClassDef]]:
    text = path.read_text(encoding="utf-8")
    return parse_source(text, source_file=str(path))
