package validate

import (
	"fmt"

	"property_sync/psync/internal/ir"
)

type Diagnostic struct {
	Level   string // error | warning
	Code    string
	Message string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s [%s] %s", toUpper(d.Level), d.Code, d.Message)
}

type RuleFn func(*ir.CompilationUnit) []Diagnostic

func implicitReserved(cls *ir.ClassDef) map[int]bool {
	switch cls.Kind {
	case "bag_item":
		return map[int]bool{0: true}
	case "slot_item":
		return map[int]bool{0: true, 1: true}
	}
	return map[int]bool{}
}

func flagNames(flags *ir.FlagsDef) map[string]bool {
	names := map[string]bool{}
	for _, b := range flags.Bits {
		names[b.Name] = true
	}
	for _, a := range flags.Aliases {
		names[a.Name] = true
	}
	return names
}

func RuleUniqueClassNames(unit *ir.CompilationUnit) []Diagnostic {
	seen := map[string]string{}
	var out []Diagnostic
	for _, cls := range unit.Classes {
		key := cls.Namespace + "::" + cls.Name
		if _, ok := seen[key]; ok {
			out = append(out, Diagnostic{"error", "V7", fmt.Sprintf("duplicate class %s", key)})
		}
		seen[key] = cls.SourceFile
	}
	return out
}

func RuleFieldIndexes(unit *ir.CompilationUnit) []Diagnostic {
	var out []Diagnostic
	for _, cls := range unit.Classes {
		reserved := map[int]bool{}
		for _, r := range cls.Reserved {
			reserved[r] = true
		}
		impl := implicitReserved(cls)
		for k := range impl {
			reserved[k] = true
		}
		used := map[int]string{}
		for _, f := range cls.Fields {
			if f.Index < 0 || f.Index > 254 {
				out = append(out, Diagnostic{"error", "V1", fmt.Sprintf("%s.%s: index %d out of range", cls.Name, f.Name, f.Index)})
			}
			if impl[f.Index] && !f.Deprecated {
				out = append(out, Diagnostic{"error", "V1", fmt.Sprintf("%s.%s: index %d reserved by %s", cls.Name, f.Name, f.Index, cls.Kind)})
			}
			if prev, ok := used[f.Index]; ok {
				out = append(out, Diagnostic{"error", "V1", fmt.Sprintf("%s: index %d used by both %s and %s", cls.Name, f.Index, prev, f.Name)})
			}
			used[f.Index] = f.Name
			for _, r := range cls.Reserved {
				if f.Index == r {
					out = append(out, Diagnostic{"error", "V5", fmt.Sprintf("%s.%s: index %d is in reserved list", cls.Name, f.Name, f.Index)})
				}
			}
		}
		for _, r := range cls.Reserved {
			if impl[r] {
				out = append(out, Diagnostic{"warning", "V5", fmt.Sprintf("%s: reserved %d overlaps implicit %s indexes", cls.Name, r, cls.Kind)})
			}
		}
	}
	return out
}

func RuleContainerItemKinds(unit *ir.CompilationUnit) []Diagnostic {
	var out []Diagnostic
	for _, cls := range unit.Classes {
		for _, f := range cls.Fields {
			t := f.Type
			itemName := ""
			if t.Name != nil {
				itemName = *t.Name
			}
			switch t.Kind {
			case "bag":
				item := unit.Classes[itemName]
				if item == nil {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: unknown bag item %s", cls.Name, f.Name, itemName)})
				} else if item.Kind != "bag_item" {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: bag<> requires bag_item, got %s", cls.Name, f.Name, item.Kind)})
				}
			case "slots":
				item := unit.Classes[itemName]
				if item == nil {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: unknown slots item %s", cls.Name, f.Name, itemName)})
				} else if item.Kind != "slot_item" {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: slots<> requires slot_item, got %s", cls.Name, f.Name, item.Kind)})
				}
			case "vec":
				item := unit.Classes[itemName]
				if item == nil {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: unknown vec item %s", cls.Name, f.Name, itemName)})
				} else if item.Kind != "vec_item" {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: vec<> requires vec_item, got %s", cls.Name, f.Name, item.Kind)})
				}
			case "object":
				item := unit.Classes[itemName]
				if item == nil {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: unknown object %s", cls.Name, f.Name, itemName)})
				} else if item.Kind != "object" && item.Kind != "entity" {
					out = append(out, Diagnostic{"error", "V2", fmt.Sprintf("%s.%s: object<> expects object/entity, got %s", cls.Name, f.Name, item.Kind)})
				}
			}
		}
	}
	return out
}

func RuleFlagsResolve(unit *ir.CompilationUnit) []Diagnostic {
	var out []Diagnostic
	for _, cls := range unit.Classes {
		if cls.FlagsRef == nil {
			for _, f := range cls.Fields {
				if len(f.Flags) > 0 {
					out = append(out, Diagnostic{"error", "V3", fmt.Sprintf("%s: fields have flags but no `using flags`", cls.Name)})
					break
				}
			}
			continue
		}
		fd := unit.Flags[*cls.FlagsRef]
		if fd == nil {
			out = append(out, Diagnostic{"error", "V3", fmt.Sprintf("%s: unknown flags %s", cls.Name, *cls.FlagsRef)})
			continue
		}
		known := flagNames(fd)
		for _, f := range cls.Fields {
			for _, name := range f.Flags {
				if !known[name] {
					out = append(out, Diagnostic{"error", "V3", fmt.Sprintf("%s.%s: unknown flag %s", cls.Name, f.Name, name)})
				}
			}
		}
	}
	for _, fd := range unit.Flags {
		known := flagNames(fd)
		for _, a := range fd.Aliases {
			if len(a.Expr) == 1 && a.Expr[0] == "*" {
				continue
			}
			for _, part := range a.Expr {
				if !known[part] || part == a.Name {
					out = append(out, Diagnostic{"error", "V3", fmt.Sprintf("flags %s: alias %s references unknown %s", fd.Name, a.Name, part)})
				}
			}
		}
	}
	return out
}

func RuleSimpleContainerElems(unit *ir.CompilationUnit) []Diagnostic {
	var out []Diagnostic
	okScalar := func(name *string) bool {
		return name != nil && ir.ScalarNames[*name]
	}
	for _, cls := range unit.Classes {
		for _, f := range cls.Fields {
			t := f.Type
			switch t.Kind {
			case "array":
				if !okScalar(t.Elem) {
					out = append(out, Diagnostic{"error", "V4", fmt.Sprintf("%s.%s: array elem must be scalar", cls.Name, f.Name)})
				}
				if t.Size == nil || *t.Size < 1 {
					out = append(out, Diagnostic{"error", "V4", fmt.Sprintf("%s.%s: array size must be >= 1", cls.Name, f.Name)})
				}
			case "list":
				if !okScalar(t.Elem) {
					out = append(out, Diagnostic{"error", "V4", fmt.Sprintf("%s.%s: list elem must be scalar", cls.Name, f.Name)})
				}
			case "dict":
				if !okScalar(t.Key) || !okScalar(t.Value) {
					out = append(out, Diagnostic{"error", "V4", fmt.Sprintf("%s.%s: dict key/value must be scalar", cls.Name, f.Name)})
				}
				if t.Key != nil {
					k := *t.Key
					if k != "string" && k != "int" && k != "int32" && k != "int64" && k != "uint32" && k != "uint64" {
						out = append(out, Diagnostic{"warning", "V4", fmt.Sprintf("%s.%s: unusual dict key type %s", cls.Name, f.Name, k)})
					}
				}
			}
		}
	}
	return out
}

func RuleVersion(unit *ir.CompilationUnit) []Diagnostic {
	var out []Diagnostic
	for _, cls := range unit.Classes {
		if cls.SchemaVersion < 1 {
			out = append(out, Diagnostic{"error", "V6", fmt.Sprintf("%s: version must be >= 1", cls.Name)})
		}
	}
	return out
}

func RuleFirstClassScalarsWarn(unit *ir.CompilationUnit) []Diagnostic {
	var out []Diagnostic
	check := func(where string, name *string) {
		if name != nil && ir.ScalarNames[*name] && !ir.FirstClassScalars[*name] {
			out = append(out, Diagnostic{"warning", "T1", fmt.Sprintf("%s: scalar %s is late-tier (see dsl-types.md)", where, *name)})
		}
	}
	for _, cls := range unit.Classes {
		if cls.KeyType != nil {
			check(cls.Name+" key", cls.KeyType)
		}
		for _, f := range cls.Fields {
			t := f.Type
			switch t.Kind {
			case "scalar":
				check(cls.Name+"."+f.Name, t.Name)
			case "array", "list":
				check(cls.Name+"."+f.Name, t.Elem)
			case "dict":
				check(cls.Name+"."+f.Name, t.Key)
				check(cls.Name+"."+f.Name, t.Value)
			}
		}
	}
	return out
}

var DefaultRules = []RuleFn{
	RuleUniqueClassNames,
	RuleFieldIndexes,
	RuleContainerItemKinds,
	RuleFlagsResolve,
	RuleSimpleContainerElems,
	RuleVersion,
	RuleFirstClassScalarsWarn,
}

func Validate(unit *ir.CompilationUnit, rules []RuleFn) []Diagnostic {
	if rules == nil {
		rules = DefaultRules
	}
	var diags []Diagnostic
	for _, rule := range rules {
		diags = append(diags, rule(unit)...)
	}
	return diags
}

func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Level == "error" {
			return true
		}
	}
	return false
}

func toUpper(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		b[i] = c
	}
	return string(b)
}
