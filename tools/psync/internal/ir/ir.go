// Package ir holds language-neutral IR models (JSON-serializable).
package ir

import "encoding/json"

var ScalarNames = map[string]bool{
	"bool": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float": true, "double": true, "string": true,
}

var FirstClassScalars = map[string]bool{
	"bool": true, "int": true, "int32": true, "int64": true,
	"uint32": true, "uint64": true, "float": true, "double": true, "string": true,
}

type FlagBit struct {
	Name string `json:"name"`
	Bit  int    `json:"bit"`
}

type FlagAlias struct {
	Name string   `json:"name"`
	Expr []string `json:"expr"`
}

type FlagsDef struct {
	Name    string      `json:"name"`
	Bits    []FlagBit   `json:"bits"`
	Aliases []FlagAlias `json:"aliases"`
}

func (f FlagsDef) ToIR() map[string]any {
	bits := make([]map[string]any, len(f.Bits))
	for i, b := range f.Bits {
		bits[i] = map[string]any{"name": b.Name, "bit": b.Bit}
	}
	aliases := make([]map[string]any, len(f.Aliases))
	for i, a := range f.Aliases {
		aliases[i] = map[string]any{"name": a.Name, "expr": a.Expr}
	}
	return map[string]any{"name": f.Name, "bits": bits, "aliases": aliases}
}

type TypeRef struct {
	Kind  string  // scalar|array|list|dict|bag|slots|vec|object
	Name  *string // scalar name or item class
	Elem  *string
	Size  *int
	Key   *string
	Value *string
}

func (t TypeRef) WireKind() string {
	switch t.Kind {
	case "scalar":
		if t.Name != nil {
			switch *t.Name {
			case "bool":
				return "bool"
			case "string":
				return "string"
			}
		}
		return "number"
	case "array", "list", "dict", "bag", "slots", "vec", "object":
		return t.Kind
	default:
		return "other"
	}
}

func (t TypeRef) ToIR() map[string]any {
	out := map[string]any{"kind": t.Kind}
	switch t.Kind {
	case "scalar":
		if t.Name != nil {
			out["name"] = *t.Name
		}
	case "array":
		if t.Elem != nil {
			out["elem"] = *t.Elem
		}
		if t.Size != nil {
			out["size"] = *t.Size
		}
	case "list":
		if t.Elem != nil {
			out["elem"] = *t.Elem
		}
	case "dict":
		if t.Key != nil {
			out["key"] = *t.Key
		}
		if t.Value != nil {
			out["value"] = *t.Value
		}
	case "bag", "slots", "vec", "object":
		if t.Name != nil {
			out["item"] = *t.Name
		}
	}
	return out
}

type FieldDef struct {
	Index            int
	Name             string
	Type             TypeRef
	Flags            []string
	Default          any
	Deprecated       bool
	DeprecatedReason *string
}

func (f FieldDef) ToIR() map[string]any {
	d := map[string]any{
		"index":      f.Index,
		"name":       f.Name,
		"type":       f.Type.ToIR(),
		"wire_kind":  f.Type.WireKind(),
		"flags":      f.Flags,
		"deprecated": f.Deprecated,
	}
	if f.Default != nil {
		d["default"] = f.Default
	}
	if f.DeprecatedReason != nil {
		d["deprecated_reason"] = *f.DeprecatedReason
	}
	if f.Type.Kind == "bag" || f.Type.Kind == "slots" || f.Type.Kind == "vec" || f.Type.Kind == "object" {
		if f.Type.Name != nil {
			d["item_class"] = *f.Type.Name
		} else {
			d["item_class"] = ""
		}
	} else {
		d["item_class"] = ""
	}
	if f.Flags == nil {
		d["flags"] = []string{}
	}
	return d
}

type ClassDef struct {
	Kind          string // entity|bag_item|slot_item|vec_item|object
	Name          string
	Namespace     string
	SchemaVersion int
	KeyType       *string
	Fields        []FieldDef
	Reserved      []int
	FlagsRef      *string
	SourceFile    string
}

func (c ClassDef) ToIR() map[string]any {
	fields := make([]FieldDef, len(c.Fields))
	copy(fields, c.Fields)
	// sort by index
	for i := 0; i < len(fields); i++ {
		for j := i + 1; j < len(fields); j++ {
			if fields[j].Index < fields[i].Index {
				fields[i], fields[j] = fields[j], fields[i]
			}
		}
	}
	fieldIRs := make([]any, len(fields))
	for i, f := range fields {
		fieldIRs[i] = f.ToIR()
	}
	reserved := append([]int{}, c.Reserved...) // non-nil empty → JSON []
	for i := 0; i < len(reserved); i++ {
		for j := i + 1; j < len(reserved); j++ {
			if reserved[j] < reserved[i] {
				reserved[i], reserved[j] = reserved[j], reserved[i]
			}
		}
	}
	return map[string]any{
		"name":           c.Name,
		"kind":           c.Kind,
		"namespace":      c.Namespace,
		"schema_version": c.SchemaVersion,
		"key_type":       c.KeyType,
		"flags_ref":      c.FlagsRef,
		"fields":         fieldIRs,
		"reserved":       reserved,
		"source_file":    c.SourceFile,
	}
}

type CompilationUnit struct {
	Entry   string
	Flags   map[string]*FlagsDef
	Classes map[string]*ClassDef
}

func NewUnit(entry string) *CompilationUnit {
	return &CompilationUnit{
		Entry:   entry,
		Flags:   map[string]*FlagsDef{},
		Classes: map[string]*ClassDef{},
	}
}

func (u *CompilationUnit) ToBundleIR() map[string]any {
	flags := map[string]any{}
	for _, k := range sortedKeys(u.Flags) {
		flags[k] = u.Flags[k].ToIR()
	}
	classes := map[string]any{}
	for _, k := range sortedKeys(u.Classes) {
		classes[k] = u.Classes[k].ToIR()
	}
	return map[string]any{
		"entry":   u.Entry,
		"flags":   flags,
		"classes": classes,
	}
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func MarshalIR(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
