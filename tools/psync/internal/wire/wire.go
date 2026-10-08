package wire

import (
	"fmt"
	"strings"

	"property_sync/psync/internal/ir"
)

var WireLegacy = map[string]string{"list": "vector", "dict": "map"}

var WireKinds = []string{
	"bag", "slots", "vec", "map", "vector", "array", "string", "number", "bool", "object", "other",
}

var CPPScalar = map[string]string{
	"bool": "bool", "int": "int", "int8": "std::int8_t", "int16": "std::int16_t",
	"int32": "std::int32_t", "int64": "std::int64_t",
	"uint8": "std::uint8_t", "uint16": "std::uint16_t", "uint32": "std::uint32_t", "uint64": "std::uint64_t",
	"float": "float", "double": "double", "string": "std::string",
}

const DefaultFlagClass = "spiritsaway::property::rpg_property_flags"

const (
	BaseBag  = "spiritsaway::property::property_bag_item"
	BaseSlot = "spiritsaway::property::property_slot_item"
	BaseVec  = "spiritsaway::property::property_vec_item"
)

func NsCpp(dotted string) string {
	if dotted == "" {
		return ""
	}
	return strings.ReplaceAll(dotted, ".", "::")
}

func Qual(ns, name string) string {
	cpp := NsCpp(ns)
	if cpp == "" {
		return name
	}
	return cpp + "::" + name
}

func MapWire(w string, legacy bool) string {
	if legacy {
		if m, ok := WireLegacy[w]; ok {
			return m
		}
	}
	return w
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func CppTypeOf(t ir.TypeRef, ns string) string {
	switch t.Kind {
	case "scalar":
		n := ptrStr(t.Name)
		if c, ok := CPPScalar[n]; ok {
			return c
		}
		return n
	case "array":
		elem := ptrStr(t.Elem)
		if c, ok := CPPScalar[elem]; ok {
			elem = c
		}
		sz := 0
		if t.Size != nil {
			sz = *t.Size
		}
		return fmt.Sprintf("std::array<%s, %d>", elem, sz)
	case "list":
		elem := ptrStr(t.Elem)
		if c, ok := CPPScalar[elem]; ok {
			elem = c
		}
		return fmt.Sprintf("std::vector<%s>", elem)
	case "dict":
		k, v := ptrStr(t.Key), ptrStr(t.Value)
		if c, ok := CPPScalar[k]; ok {
			k = c
		}
		if c, ok := CPPScalar[v]; ok {
			v = c
		}
		return fmt.Sprintf("std::unordered_map<%s, %s>", k, v)
	case "bag", "slots", "vec", "object":
		item := Qual(ns, ptrStr(t.Name))
		if t.Kind == "object" {
			return item
		}
		prefix := map[string]string{
			"bag":   "spiritsaway::property::property_bag",
			"slots": "spiritsaway::property::property_slots",
			"vec":   "spiritsaway::property::property_vec",
		}[t.Kind]
		return fmt.Sprintf("%s<%s>", prefix, item)
	}
	return "?"
}

func ItemClassOf(t ir.TypeRef, ns string) string {
	if (t.Kind == "bag" || t.Kind == "slots" || t.Kind == "vec" || t.Kind == "object") && t.Name != nil {
		return Qual(ns, *t.Name)
	}
	return ""
}

func HasPropertyInterface(t ir.TypeRef) bool {
	return t.Kind == "bag" || t.Kind == "slots" || t.Kind == "vec" || t.Kind == "object"
}

func PropertyIdxBegin(kind string) int {
	switch kind {
	case "bag_item":
		return 1
	case "slot_item":
		return 2
	}
	return 0
}

func BaseClassName(cls *ir.ClassDef) string {
	key := "int"
	if cls.KeyType != nil {
		key = *cls.KeyType
	}
	ck := key
	if c, ok := CPPScalar[key]; ok {
		ck = c
	}
	switch cls.Kind {
	case "bag_item":
		return fmt.Sprintf("%s<%s>", BaseBag, ck)
	case "slot_item":
		return fmt.Sprintf("%s<%s>", BaseSlot, ck)
	case "vec_item":
		return BaseVec
	}
	return ""
}

func WireKindFlags(w string) map[string]bool {
	out := map[string]bool{}
	for _, k := range WireKinds {
		out["is_wire_"+k] = w == k
	}
	return out
}
