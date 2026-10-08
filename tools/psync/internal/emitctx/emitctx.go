package emitctx

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"property_sync/psync/internal/ir"
	"property_sync/psync/internal/wire"
)

func ResolveFlagExpr(names []string, flagClass string) string {
	if flagClass == "" {
		flagClass = wire.DefaultFlagClass
	}
	if len(names) == 0 {
		return "0"
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = flagClass + "::" + n
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "|" + parts[i]
	}
	return out
}

func protoTypeOfScalar(name string) string {
	switch name {
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "float", "double":
		return "float"
	}
	return "int64"
}

func protoTypeOf(f ir.FieldDef) string {
	t := f.Type
	switch t.Kind {
	case "scalar":
		return protoTypeOfScalar(deref(t.Name))
	case "array":
		return "repeated " + protoTypeOfScalar(or(deref(t.Elem), "float"))
	case "list":
		return "repeated " + protoTypeOfScalar(or(deref(t.Elem), "string"))
	case "dict":
		return "map<string, int64>"
	case "bag":
		return "repeated " + deref(t.Name)
	case "slots":
		return deref(t.Name) + "Slots"
	case "vec":
		return "repeated " + deref(t.Name)
	case "object":
		return deref(t.Name)
	}
	return "bytes"
}

func FieldToMustache(f ir.FieldDef, cls *ir.ClassDef, first bool, legacyWire bool, flagClass string) map[string]any {
	if flagClass == "" {
		flagClass = wire.DefaultFlagClass
	}
	w := wire.MapWire(f.Type.WireKind(), legacyWire)
	itemQ := wire.ItemClassOf(f.Type, cls.Namespace)
	itemShort := ""
	if f.Type.Kind == "bag" || f.Type.Kind == "slots" || f.Type.Kind == "vec" || f.Type.Kind == "object" {
		itemShort = deref(f.Type.Name)
	}
	hasItem := (f.Type.Kind == "bag" || f.Type.Kind == "slots" || f.Type.Kind == "vec") && itemShort != ""
	flagNames := make([]map[string]any, len(f.Flags))
	for i, name := range f.Flags {
		flagNames[i] = map[string]any{"flag_name": name, "first": i == 0}
	}
	d := map[string]any{
		"field_name":              f.Name,
		"first_field":             first,
		"field_index":             strconv.Itoa(f.Index),
		"proto_field_number":      strconv.Itoa(f.Index + 1),
		"has_property_interface":  wire.HasPropertyInterface(f.Type),
		"field_cpp_type":          wire.CppTypeOf(f.Type, cls.Namespace),
		"wire_kind":               w,
		"item_class":              itemQ,
		"proto_type":              protoTypeOf(f),
		"has_item_meta":           hasItem,
		"item_meta_module":        "",
		"item_meta_local":         "",
		"field_flags":             ResolveFlagExpr(f.Flags, flagClass),
		"flag_names":              flagNames,
	}
	if hasItem {
		d["item_meta_module"] = itemShort + "_meta"
		d["item_meta_local"] = itemShort + "Meta"
	}
	for k, v := range wire.WireKindFlags(w) {
		d[k] = v
	}
	return d
}

func ClassToMustache(cls *ir.ClassDef, legacyWire bool, flagClass string) map[string]any {
	if flagClass == "" {
		flagClass = wire.DefaultFlagClass
	}
	fields := append([]ir.FieldDef(nil), cls.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Index < fields[j].Index })
	begin := wire.PropertyIdxBegin(cls.Kind)
	idxMax := begin
	if len(fields) > 0 {
		idxMax = fields[0].Index
		for _, f := range fields {
			if f.Index > idxMax {
				idxMax = f.Index
			}
		}
		idxMax++
	}
	base := wire.BaseClassName(cls)
	isItem := cls.Kind == "bag_item" || cls.Kind == "slot_item" || cls.Kind == "vec_item"
	var protoImports []map[string]any
	seen := map[string]bool{}
	for _, f := range fields {
		if f.Type.Kind == "bag" || f.Type.Kind == "slots" || f.Type.Kind == "vec" || f.Type.Kind == "object" {
			if f.Type.Name == nil {
				continue
			}
			if f.Type.Kind == "object" && *f.Type.Name == cls.Name {
				continue
			}
			file := toSnakeProto(*f.Type.Name) + ".proto"
			if !seen[file] {
				seen[file] = true
				protoImports = append(protoImports, map[string]any{"import_file": file})
			}
		}
	}
	pfields := make([]map[string]any, len(fields))
	for i, f := range fields {
		pfields[i] = FieldToMustache(f, cls, i == 0, legacyWire, flagClass)
	}
	return map[string]any{
		"class_name":                       cls.Name,
		"class_full_name":                  wire.Qual(cls.Namespace, cls.Name),
		"class_namespace":                  wire.NsCpp(cls.Namespace),
		"schema_version":                   strconv.Itoa(cls.SchemaVersion),
		"has_base_class":                   base != "",
		"base_class_name":                  base,
		"is_bag_item":                      cls.Kind == "bag_item",
		"is_slot_item":                     cls.Kind == "slot_item",
		"is_vec_item":                      cls.Kind == "vec_item",
		"is_property_item":                 isItem,
		"is_property_item_direct_subclass": isItem,
		"property_idx_begin":               strconv.Itoa(begin),
		"property_idx_max":                 strconv.Itoa(idxMax),
		"property_fields":                  pfields,
		"proto_imports":                    protoImports,
	}
}

func FlagsToMustache(flags *ir.FlagsDef) map[string]any {
	bits := make([]map[string]any, len(flags.Bits))
	for i, b := range flags.Bits {
		bits[i] = map[string]any{"name": b.Name, "bit": b.Bit, "first": i == 0}
	}
	aliases := make([]map[string]any, len(flags.Aliases))
	for i, a := range flags.Aliases {
		isMask := len(a.Expr) == 1 && a.Expr[0] == "*"
		var parts []map[string]any
		expr := ""
		if isMask {
			expr = "std::numeric_limits<std::uint64_t>::max()"
		} else {
			parts = make([]map[string]any, len(a.Expr))
			for j, n := range a.Expr {
				parts[j] = map[string]any{"name": n, "first": j == 0}
			}
			expr = joinPipe(a.Expr)
		}
		aliases[i] = map[string]any{
			"name": a.Name, "expr": expr, "parts": parts,
			"is_mask_all": isMask, "first": i == 0,
		}
	}
	return map[string]any{
		"flags_dsl_name":   flags.Name,
		"flag_struct_name": "rpg_property_flags",
		"flag_enum_name":   "rpg_property_flags_enum",
		"bits":             bits,
		"aliases":          aliases,
		"namespace":        "spiritsaway::property",
	}
}

func DefaultFlags(unit *ir.CompilationUnit) *ir.FlagsDef {
	if len(unit.Flags) == 0 {
		return nil
	}
	for _, cls := range unit.Classes {
		if cls.FlagsRef != nil {
			if fd := unit.Flags[*cls.FlagsRef]; fd != nil {
				return fd
			}
		}
	}
	for _, fd := range unit.Flags {
		return fd
	}
	return nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func joinPipe(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	for i := 1; i < len(parts); i++ {
		s += " | " + parts[i]
	}
	return s
}

// toSnakeProto: Player → player, LoginRecord → login_record (proto file stem).
func toSnakeProto(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
