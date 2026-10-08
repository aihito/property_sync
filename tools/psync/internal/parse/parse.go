// Package parse: recursive-descent parser tokens → IR fragments.
package parse

import (
	"fmt"
	"os"
	"strconv"

	"property_sync/psync/internal/ir"
	"property_sync/psync/internal/lex"
)

type Error struct {
	Msg  string
	Line int
	Col  int
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
	}
	return e.Msg
}

type Parser struct {
	tokens     []lex.Token
	pos        int
	sourceFile string
	namespace  string
	flagsRef   *string
	Imports    []string
	Flags      []ir.FlagsDef
	Classes    []ir.ClassDef
}

func New(tokens []lex.Token, sourceFile string) *Parser {
	return &Parser{tokens: tokens, sourceFile: sourceFile}
}

func (p *Parser) cur() lex.Token { return p.tokens[p.pos] }

func (p *Parser) check(kinds ...string) bool {
	k := p.cur().Kind
	for _, x := range kinds {
		if k == x {
			return true
		}
	}
	return false
}

func (p *Parser) advance() lex.Token {
	tok := p.cur()
	if tok.Kind != "EOF" {
		p.pos++
	}
	return tok
}

func (p *Parser) expect(kinds ...string) (lex.Token, error) {
	if !p.check(kinds...) {
		return lex.Token{}, &Error{
			Msg:  fmt.Sprintf("expected %v, got %s (%q)", kinds, p.cur().Kind, p.cur().Value),
			Line: p.cur().Line, Col: p.cur().Col,
		}
	}
	return p.advance(), nil
}

func (p *Parser) match(kinds ...string) *lex.Token {
	if p.check(kinds...) {
		t := p.advance()
		return &t
	}
	return nil
}

func (p *Parser) ParseFile() error {
	for !p.check("EOF") {
		switch {
		case p.check("IMPORT"):
			if err := p.parseImport(); err != nil {
				return err
			}
		case p.check("FLAGS"):
			fd, err := p.parseFlags()
			if err != nil {
				return err
			}
			p.Flags = append(p.Flags, fd)
		case p.check("NAMESPACE"):
			if err := p.parseNamespace(); err != nil {
				return err
			}
		case p.check("USING"):
			if err := p.parseUsing(); err != nil {
				return err
			}
		case p.check("ENTITY", "BAG_ITEM", "SLOT_ITEM", "VEC_ITEM", "OBJECT"):
			cls, err := p.parseClass()
			if err != nil {
				return err
			}
			p.Classes = append(p.Classes, cls)
		default:
			return &Error{Msg: fmt.Sprintf("unexpected token %s", p.cur().Kind), Line: p.cur().Line, Col: p.cur().Col}
		}
	}
	return nil
}

func (p *Parser) parseImport() error {
	if _, err := p.expect("IMPORT"); err != nil {
		return err
	}
	tok, err := p.expect("STRING")
	if err != nil {
		return err
	}
	p.Imports = append(p.Imports, tok.Value)
	return nil
}

func (p *Parser) parseNamespace() error {
	if _, err := p.expect("NAMESPACE"); err != nil {
		return err
	}
	tok, err := p.expect("IDENT")
	if err != nil {
		return err
	}
	parts := []string{tok.Value}
	for p.match("DOT") != nil {
		t, err := p.expect("IDENT")
		if err != nil {
			return err
		}
		parts = append(parts, t.Value)
	}
	p.namespace = joinDot(parts)
	return nil
}

func (p *Parser) parseUsing() error {
	if _, err := p.expect("USING"); err != nil {
		return err
	}
	if _, err := p.expect("FLAGS"); err != nil {
		return err
	}
	tok, err := p.expect("IDENT")
	if err != nil {
		return err
	}
	v := tok.Value
	p.flagsRef = &v
	return nil
}

func (p *Parser) parseFlags() (ir.FlagsDef, error) {
	var zero ir.FlagsDef
	if _, err := p.expect("FLAGS"); err != nil {
		return zero, err
	}
	nameTok, err := p.expect("IDENT")
	if err != nil {
		return zero, err
	}
	if _, err := p.expect("LBRACE"); err != nil {
		return zero, err
	}
	var bits []ir.FlagBit
	var aliases []ir.FlagAlias
	for !p.check("RBRACE") {
		if p.match("BIT") != nil {
			bn, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("EQ"); err != nil {
				return zero, err
			}
			num, err := p.expect("NUMBER")
			if err != nil {
				return zero, err
			}
			bit, _ := strconv.Atoi(num.Value)
			bits = append(bits, ir.FlagBit{Name: bn.Value, Bit: bit})
		} else if p.match("ALIAS") != nil {
			an, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("EQ"); err != nil {
				return zero, err
			}
			expr, err := p.parseAliasExpr()
			if err != nil {
				return zero, err
			}
			aliases = append(aliases, ir.FlagAlias{Name: an.Value, Expr: expr})
		} else {
			return zero, &Error{Msg: "expected bit or alias", Line: p.cur().Line, Col: p.cur().Col}
		}
	}
	if _, err := p.expect("RBRACE"); err != nil {
		return zero, err
	}
	return ir.FlagsDef{Name: nameTok.Value, Bits: bits, Aliases: aliases}, nil
}

func (p *Parser) parseAliasExpr() ([]string, error) {
	if p.match("STAR") != nil {
		return []string{"*"}, nil
	}
	tok, err := p.expect("IDENT")
	if err != nil {
		return nil, err
	}
	names := []string{tok.Value}
	for p.match("PIPE") != nil {
		t, err := p.expect("IDENT")
		if err != nil {
			return nil, err
		}
		names = append(names, t.Value)
	}
	return names, nil
}

func (p *Parser) parseClass() (ir.ClassDef, error) {
	var zero ir.ClassDef
	kindTok, err := p.expect("ENTITY", "BAG_ITEM", "SLOT_ITEM", "VEC_ITEM", "OBJECT")
	if err != nil {
		return zero, err
	}
	kindMap := map[string]string{
		"ENTITY": "entity", "BAG_ITEM": "bag_item", "SLOT_ITEM": "slot_item",
		"VEC_ITEM": "vec_item", "OBJECT": "object",
	}
	kind := kindMap[kindTok.Kind]
	nameTok, err := p.expect("IDENT")
	if err != nil {
		return zero, err
	}
	if _, err := p.expect("LBRACE"); err != nil {
		return zero, err
	}
	version := 1
	var keyType *string
	var fields []ir.FieldDef
	var reserved []int
	for !p.check("RBRACE") {
		switch {
		case p.match("VERSION") != nil:
			num, err := p.expect("NUMBER")
			if err != nil {
				return zero, err
			}
			version, _ = strconv.Atoi(num.Value)
		case p.match("KEY") != nil:
			kt, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if !ir.ScalarNames[kt.Value] {
				return zero, &Error{Msg: fmt.Sprintf("invalid key type %s", kt.Value), Line: p.cur().Line, Col: p.cur().Col}
			}
			v := kt.Value
			keyType = &v
		case p.match("RESERVED") != nil:
			rs, err := p.parseReservedList()
			if err != nil {
				return zero, err
			}
			reserved = append(reserved, rs...)
		case p.check("NUMBER"):
			f, err := p.parseField()
			if err != nil {
				return zero, err
			}
			fields = append(fields, f)
		default:
			return zero, &Error{Msg: "expected version, key, reserved, or field", Line: p.cur().Line, Col: p.cur().Col}
		}
	}
	if _, err := p.expect("RBRACE"); err != nil {
		return zero, err
	}
	if (kind == "bag_item" || kind == "slot_item") && keyType == nil {
		return zero, &Error{Msg: fmt.Sprintf("%s %s requires key <type>", kind, nameTok.Value), Line: kindTok.Line, Col: kindTok.Col}
	}
	return ir.ClassDef{
		Kind: kind, Name: nameTok.Value, Namespace: p.namespace,
		SchemaVersion: version, KeyType: keyType, Fields: fields,
		Reserved: reserved, FlagsRef: p.flagsRef, SourceFile: p.sourceFile,
	}, nil
}

func (p *Parser) parseReservedList() ([]int, error) {
	var indexes []int
	item, err := p.parseReservedItem()
	if err != nil {
		return nil, err
	}
	indexes = append(indexes, item...)
	for p.match("COMMA") != nil {
		item, err = p.parseReservedItem()
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, item...)
	}
	return indexes, nil
}

func (p *Parser) parseReservedItem() ([]int, error) {
	startTok, err := p.expect("NUMBER")
	if err != nil {
		return nil, err
	}
	start, _ := strconv.Atoi(startTok.Value)
	if p.match("TO") != nil {
		endTok, err := p.expect("NUMBER")
		if err != nil {
			return nil, err
		}
		end, _ := strconv.Atoi(endTok.Value)
		if end < start {
			return nil, &Error{Msg: fmt.Sprintf("reserved range %d to %d invalid", start, end), Line: p.cur().Line, Col: p.cur().Col}
		}
		out := make([]int, 0, end-start+1)
		for i := start; i <= end; i++ {
			out = append(out, i)
		}
		return out, nil
	}
	return []int{start}, nil
}

func (p *Parser) parseField() (ir.FieldDef, error) {
	var zero ir.FieldDef
	idxTok, err := p.expect("NUMBER")
	if err != nil {
		return zero, err
	}
	index, _ := strconv.Atoi(idxTok.Value)
	if _, err := p.expect("COLON"); err != nil {
		return zero, err
	}
	nameTok, err := p.expect("IDENT")
	if err != nil {
		return zero, err
	}
	typ, err := p.parseType()
	if err != nil {
		return zero, err
	}
	var def any
	if p.match("EQ") != nil {
		def, err = p.parseDefault(typ)
		if err != nil {
			return zero, err
		}
	}
	var flags []string
	if p.match("LBRACK") != nil {
		if !p.check("RBRACK") {
			ft, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			flags = append(flags, ft.Value)
			for p.match("COMMA") != nil {
				ft, err = p.expect("IDENT")
				if err != nil {
					return zero, err
				}
				flags = append(flags, ft.Value)
			}
		}
		if _, err := p.expect("RBRACK"); err != nil {
			return zero, err
		}
	}
	deprecated := false
	var reason *string
	if p.match("DEPRECATED") != nil {
		deprecated = true
		if p.check("STRING") {
			t := p.advance()
			reason = &t.Value
		}
	}
	if flags == nil {
		flags = []string{}
	}
	return ir.FieldDef{
		Index: index, Name: nameTok.Value, Type: typ, Flags: flags,
		Default: def, Deprecated: deprecated, DeprecatedReason: reason,
	}, nil
}

func (p *Parser) parseType() (ir.TypeRef, error) {
	var zero ir.TypeRef
	nameTok, err := p.expect("IDENT")
	if err != nil {
		return zero, err
	}
	name := nameTok.Value
	if name == "map" {
		name = "dict"
	}
	switch name {
	case "array", "list", "dict", "bag", "slots", "vec", "object":
		if _, err := p.expect("LT"); err != nil {
			return zero, err
		}
		switch name {
		case "array":
			elem, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("COMMA"); err != nil {
				return zero, err
			}
			sz, err := p.expect("NUMBER")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("GT"); err != nil {
				return zero, err
			}
			e := elem.Value
			size, _ := strconv.Atoi(sz.Value)
			return ir.TypeRef{Kind: "array", Elem: &e, Size: &size}, nil
		case "list":
			elem, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("GT"); err != nil {
				return zero, err
			}
			e := elem.Value
			return ir.TypeRef{Kind: "list", Elem: &e}, nil
		case "dict":
			key, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("COMMA"); err != nil {
				return zero, err
			}
			val, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("GT"); err != nil {
				return zero, err
			}
			k, v := key.Value, val.Value
			return ir.TypeRef{Kind: "dict", Key: &k, Value: &v}, nil
		default:
			item, err := p.expect("IDENT")
			if err != nil {
				return zero, err
			}
			if _, err := p.expect("GT"); err != nil {
				return zero, err
			}
			it := item.Value
			return ir.TypeRef{Kind: name, Name: &it}, nil
		}
	}
	if !ir.ScalarNames[name] {
		return zero, &Error{Msg: fmt.Sprintf("unknown type %s", name), Line: p.cur().Line, Col: p.cur().Col}
	}
	n := name
	return ir.TypeRef{Kind: "scalar", Name: &n}, nil
}

func (p *Parser) parseDefault(typ ir.TypeRef) (any, error) {
	if p.check("TRUE") {
		p.advance()
		return true, nil
	}
	if p.check("FALSE") {
		p.advance()
		return false, nil
	}
	if p.check("STRING") {
		return p.advance().Value, nil
	}
	if p.check("NUMBER") {
		text := p.advance().Value
		if containsDot(text) {
			f, _ := strconv.ParseFloat(text, 64)
			return f, nil
		}
		i, _ := strconv.Atoi(text)
		return i, nil
	}
	if p.match("LBRACE") != nil {
		var vals []any
		if !p.check("RBRACE") {
			v, err := p.parseDefaultScalar()
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
			for p.match("COMMA") != nil {
				v, err = p.parseDefaultScalar()
				if err != nil {
					return nil, err
				}
				vals = append(vals, v)
			}
		}
		if _, err := p.expect("RBRACE"); err != nil {
			return nil, err
		}
		if typ.Kind == "array" && typ.Size != nil && len(vals) != *typ.Size {
			return nil, &Error{Msg: fmt.Sprintf("array default length %d != %d", len(vals), *typ.Size), Line: p.cur().Line, Col: p.cur().Col}
		}
		return vals, nil
	}
	return nil, &Error{Msg: "invalid default", Line: p.cur().Line, Col: p.cur().Col}
}

func (p *Parser) parseDefaultScalar() (any, error) {
	if p.check("TRUE") {
		p.advance()
		return true, nil
	}
	if p.check("FALSE") {
		p.advance()
		return false, nil
	}
	if p.check("STRING") {
		return p.advance().Value, nil
	}
	if p.check("NUMBER") {
		text := p.advance().Value
		if containsDot(text) {
			f, _ := strconv.ParseFloat(text, 64)
			return f, nil
		}
		i, _ := strconv.Atoi(text)
		return i, nil
	}
	return nil, &Error{Msg: "expected scalar in default list", Line: p.cur().Line, Col: p.cur().Col}
}

func ParseSource(src, sourceFile string) ([]string, []ir.FlagsDef, []ir.ClassDef, error) {
	tokens, err := lex.Tokenize(src)
	if err != nil {
		return nil, nil, nil, &Error{Msg: err.Error()}
	}
	p := New(tokens, sourceFile)
	if err := p.ParseFile(); err != nil {
		return nil, nil, nil, err
	}
	return p.Imports, p.Flags, p.Classes, nil
}

func ParsePath(path string) ([]string, []ir.FlagsDef, []ir.ClassDef, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	return ParseSource(string(b), path)
}

func joinDot(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	for i := 1; i < len(parts); i++ {
		s += "." + parts[i]
	}
	return s
}

func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
