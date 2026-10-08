"""Tokenizer for .psync — small, explicit token set."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Iterator, List


@dataclass(frozen=True)
class Token:
    kind: str
    value: str
    line: int
    col: int


KEYWORDS = frozenset(
    {
        "import",
        "flags",
        "bit",
        "alias",
        "namespace",
        "using",
        "entity",
        "bag_item",
        "slot_item",
        "vec_item",
        "object",
        "version",
        "key",
        "reserved",
        "to",
        "deprecated",
        "true",
        "false",
    }
)

SINGLE = {
    "{": "LBRACE",
    "}": "RBRACE",
    "<": "LT",
    ">": "GT",
    "[": "LBRACK",
    "]": "RBRACK",
    "(": "LPAREN",
    ")": "RPAREN",
    ":": "COLON",
    "=": "EQ",
    ",": "COMMA",
    "|": "PIPE",
    "*": "STAR",
    ".": "DOT",
}


class LexerError(Exception):
    def __init__(self, message: str, line: int, col: int):
        super().__init__(f"{line}:{col}: {message}")
        self.line = line
        self.col = col


def tokenize(src: str, filename: str = "<stdin>") -> List[Token]:
    tokens: List[Token] = []
    i = 0
    line = 1
    col = 1
    n = len(src)

    def peek(k: int = 0) -> str:
        j = i + k
        return src[j] if j < n else "\0"

    def advance() -> str:
        nonlocal i, line, col
        ch = src[i]
        i += 1
        if ch == "\n":
            line += 1
            col = 1
        else:
            col += 1
        return ch

    while i < n:
        ch = peek()
        start_line, start_col = line, col

        # whitespace
        if ch in " \t\r":
            advance()
            continue
        if ch == "\n":
            advance()
            continue

        # comments
        if ch == "#":
            while peek() not in ("\n", "\0"):
                advance()
            continue
        if ch == "/" and peek(1) == "/":
            advance()
            advance()
            while peek() not in ("\n", "\0"):
                advance()
            continue

        # string
        if ch == '"':
            advance()
            buf: List[str] = []
            while True:
                c = peek()
                if c == "\0":
                    raise LexerError("unterminated string", start_line, start_col)
                if c == '"':
                    advance()
                    break
                if c == "\\":
                    advance()
                    esc = advance()
                    mapping = {"n": "\n", "t": "\t", "r": "\r", '"': '"', "\\": "\\"}
                    buf.append(mapping.get(esc, esc))
                else:
                    buf.append(advance())
            tokens.append(Token("STRING", "".join(buf), start_line, start_col))
            continue

        # number
        if ch.isdigit() or (ch == "-" and peek(1).isdigit()):
            buf = []
            if ch == "-":
                buf.append(advance())
            while peek().isdigit():
                buf.append(advance())
            if peek() == "." and peek(1).isdigit():
                buf.append(advance())
                while peek().isdigit():
                    buf.append(advance())
            tokens.append(Token("NUMBER", "".join(buf), start_line, start_col))
            continue

        # ident / keyword
        if ch.isalpha() or ch == "_":
            buf = []
            while peek().isalnum() or peek() == "_":
                buf.append(advance())
            text = "".join(buf)
            kind = text if text in KEYWORDS else "IDENT"
            # keyword tokens use uppercase kind matching keyword
            if text in KEYWORDS:
                tokens.append(Token(text.upper(), text, start_line, start_col))
            else:
                tokens.append(Token("IDENT", text, start_line, start_col))
            continue

        if ch in SINGLE:
            advance()
            tokens.append(Token(SINGLE[ch], ch, start_line, start_col))
            continue

        raise LexerError(f"unexpected character {ch!r}", start_line, start_col)

    tokens.append(Token("EOF", "", line, col))
    return tokens


def iter_tokens(src: str) -> Iterator[Token]:
    yield from tokenize(src)
