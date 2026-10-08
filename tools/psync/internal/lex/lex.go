// Package lex tokenizes .psync sources.
package lex

import (
	"fmt"
	"unicode"
)

type Token struct {
	Kind  string
	Value string
	Line  int
	Col   int
}

var keywords = map[string]bool{
	"import": true, "flags": true, "bit": true, "alias": true,
	"namespace": true, "using": true, "entity": true, "bag_item": true,
	"slot_item": true, "vec_item": true, "object": true, "version": true,
	"key": true, "reserved": true, "to": true, "deprecated": true,
	"true": true, "false": true,
}

var singles = map[rune]string{
	'{': "LBRACE", '}': "RBRACE", '<': "LT", '>': "GT",
	'[': "LBRACK", ']': "RBRACK", '(': "LPAREN", ')': "RPAREN",
	':': "COLON", '=': "EQ", ',': "COMMA", '|': "PIPE",
	'*': "STAR", '.': "DOT",
}

type Error struct {
	Msg  string
	Line int
	Col  int
}

func (e *Error) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg) }

func Tokenize(src string) ([]Token, error) {
	var tokens []Token
	i, line, col := 0, 1, 1
	n := len(src)

	// Byte-oriented peek matching Python (DSL is ASCII).
	peekB := func(k int) byte {
		j := i + k
		if j >= n {
			return 0
		}
		return src[j]
	}
	advance := func() byte {
		ch := src[i]
		i++
		if ch == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		return ch
	}

	for i < n {
		ch := peekB(0)
		startLine, startCol := line, col

		if ch == ' ' || ch == '\t' || ch == '\r' {
			advance()
			continue
		}
		if ch == '\n' {
			advance()
			continue
		}
		if ch == '#' {
			for peekB(0) != '\n' && peekB(0) != 0 {
				advance()
			}
			continue
		}
		if ch == '/' && peekB(1) == '/' {
			advance()
			advance()
			for peekB(0) != '\n' && peekB(0) != 0 {
				advance()
			}
			continue
		}
		if ch == '"' {
			advance()
			var buf []byte
			for {
				c := peekB(0)
				if c == 0 {
					return nil, &Error{Msg: "unterminated string", Line: startLine, Col: startCol}
				}
				if c == '"' {
					advance()
					break
				}
				if c == '\\' {
					advance()
					esc := advance()
					switch esc {
					case 'n':
						buf = append(buf, '\n')
					case 't':
						buf = append(buf, '\t')
					case 'r':
						buf = append(buf, '\r')
					case '"':
						buf = append(buf, '"')
					case '\\':
						buf = append(buf, '\\')
					default:
						buf = append(buf, esc)
					}
				} else {
					buf = append(buf, advance())
				}
			}
			tokens = append(tokens, Token{Kind: "STRING", Value: string(buf), Line: startLine, Col: startCol})
			continue
		}
		if isDigit(ch) || (ch == '-' && isDigit(peekB(1))) {
			var buf []byte
			if ch == '-' {
				buf = append(buf, advance())
			}
			for isDigit(peekB(0)) {
				buf = append(buf, advance())
			}
			if peekB(0) == '.' && isDigit(peekB(1)) {
				buf = append(buf, advance())
				for isDigit(peekB(0)) {
					buf = append(buf, advance())
				}
			}
			tokens = append(tokens, Token{Kind: "NUMBER", Value: string(buf), Line: startLine, Col: startCol})
			continue
		}
		if isAlpha(ch) || ch == '_' {
			var buf []byte
			for isAlnum(peekB(0)) || peekB(0) == '_' {
				buf = append(buf, advance())
			}
			text := string(buf)
			if keywords[text] {
				kind := ""
				for _, r := range text {
					kind += string(unicode.ToUpper(r))
				}
				tokens = append(tokens, Token{Kind: kind, Value: text, Line: startLine, Col: startCol})
			} else {
				tokens = append(tokens, Token{Kind: "IDENT", Value: text, Line: startLine, Col: startCol})
			}
			continue
		}
		if kind, ok := singles[rune(ch)]; ok {
			advance()
			tokens = append(tokens, Token{Kind: kind, Value: string(ch), Line: startLine, Col: startCol})
			continue
		}
		return nil, &Error{Msg: fmt.Sprintf("unexpected character %q", ch), Line: startLine, Col: startCol}
	}
	tokens = append(tokens, Token{Kind: "EOF", Value: "", Line: line, Col: col})
	return tokens, nil
}

func isDigit(b byte) bool  { return b >= '0' && b <= '9' }
func isAlpha(b byte) bool  { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isAlnum(b byte) bool  { return isAlpha(b) || isDigit(b) }
