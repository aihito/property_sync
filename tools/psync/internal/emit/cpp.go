package emit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cbroglie/mustache"

	"property_sync/psync/internal/emitctx"
	"property_sync/psync/internal/ir"
	"property_sync/psync/internal/wire"
)

func mustacheDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return filepath.Join(findRepoRoot(), "meta", "mustache")
}

func renderTemplate(path string, ctx map[string]any) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return mustache.Render(string(b), ctx)
}

func defaultExpr(f ir.FieldDef) string {
	if f.Default == nil {
		return ""
	}
	t := f.Type
	v := f.Default
	if t.Kind == "scalar" && deref(t.Name) == "string" {
		s := strings.ReplaceAll(fmt.Sprint(v), `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return `= "` + s + `"`
	}
	if t.Kind == "scalar" && deref(t.Name) == "bool" {
		if b, ok := v.(bool); ok && b {
			return "= true"
		}
		return "= false"
	}
	if t.Kind == "scalar" && (deref(t.Name) == "float" || deref(t.Name) == "double") {
		switch x := v.(type) {
		case float64:
			if x == 0 {
				if deref(t.Name) == "float" {
					return "= 0.f"
				}
				return "= 0.0"
			}
			if deref(t.Name) == "float" {
				return fmt.Sprintf("= %vf", x)
			}
			return fmt.Sprintf("= %v", x)
		case int:
			if x == 0 {
				if deref(t.Name) == "float" {
					return "= 0.f"
				}
				return "= 0.0"
			}
		}
		return fmt.Sprintf("= %v", v)
	}
	if t.Kind == "scalar" {
		return fmt.Sprintf("= %v", v)
	}
	if t.Kind == "array" {
		return "= {}"
	}
	return ""
}

func headerContext(cls *ir.ClassDef, ctx map[string]any) map[string]any {
	isEntity := cls.Kind == "entity" || cls.Kind == "object"
	fields := append([]ir.FieldDef(nil), cls.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Index < fields[j].Index })
	members := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		de := defaultExpr(f)
		members = append(members, map[string]any{
			"field_name":     f.Name,
			"field_cpp_type": wire.CppTypeOf(f.Type, cls.Namespace),
			"has_default":    de != "",
			"default_expr":   de,
		})
	}
	var itemIncludes []map[string]any
	if isEntity {
		seen := map[string]bool{}
		for _, f := range fields {
			if (f.Type.Kind == "bag" || f.Type.Kind == "slots" || f.Type.Kind == "vec" || f.Type.Kind == "object") && f.Type.Name != nil {
				n := *f.Type.Name
				if !seen[n] {
					seen[n] = true
					itemIncludes = append(itemIncludes, map[string]any{"include_file": n + ".h"})
				}
			}
		}
	}
	out := map[string]any{}
	for k, v := range ctx {
		out[k] = v
	}
	out["is_entity"] = isEntity
	out["member_fields"] = members
	out["item_includes"] = itemIncludes
	return out
}

func renderClassArtifacts(cls *ir.ClassDef, md string, legacyWire bool) (map[string]string, error) {
	ctx := emitctx.ClassToMustache(cls, legacyWire, "")
	body, err := renderTemplate(filepath.Join(md, "property_h.mustache"), ctx)
	if err != nil {
		return nil, fmt.Errorf("property_h.mustache: %w", err)
	}
	proxy, err := renderTemplate(filepath.Join(md, "property_proxy_h.mustache"), ctx)
	if err != nil {
		return nil, fmt.Errorf("property_proxy_h.mustache: %w", err)
	}
	impl, err := renderTemplate(filepath.Join(md, "property_cpp.mustache"), ctx)
	if err != nil {
		return nil, fmt.Errorf("property_cpp.mustache: %w", err)
	}
	hdr := headerContext(cls, ctx)
	hdr["class_body"] = body
	hdr["proxy_body"] = proxy
	fullH, err := renderTemplate(filepath.Join(md, "class_header_full.mustache"), hdr)
	if err != nil {
		return nil, fmt.Errorf("class_header_full.mustache: %w", err)
	}
	fullCpp, err := renderTemplate(filepath.Join(md, "class_cpp_full.mustache"), map[string]any{
		"class_name": cls.Name,
		"class_impl": impl,
	})
	if err != nil {
		return nil, fmt.Errorf("class_cpp_full.mustache: %w", err)
	}
	return map[string]string{
		cls.Name + ".h":   fullH,
		cls.Name + ".cpp": fullCpp,
	}, nil
}

// RenderClassFragments renders Meta-style inch/incpp bodies (for golden tests).
func RenderClassFragments(cls *ir.ClassDef, md string, legacyWire bool) (inch, proxy, incpp string, err error) {
	ctx := emitctx.ClassToMustache(cls, legacyWire, "")
	inch, err = renderTemplate(filepath.Join(md, "property_h.mustache"), ctx)
	if err != nil {
		return "", "", "", err
	}
	proxy, err = renderTemplate(filepath.Join(md, "property_proxy_h.mustache"), ctx)
	if err != nil {
		return "", "", "", err
	}
	incpp, err = renderTemplate(filepath.Join(md, "property_cpp.mustache"), ctx)
	if err != nil {
		return "", "", "", err
	}
	return inch, proxy, incpp, nil
}

func writeCpp(unit *ir.CompilationUnit, outDir string, md string, legacyWire, flat bool) ([]string, error) {
	root := outDir
	if !flat {
		root = filepath.Join(outDir, "cpp")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	var written []string
	if fd := emitctx.DefaultFlags(unit); fd != nil {
		text, err := renderTemplate(filepath.Join(md, "flags_h.mustache"), emitctx.FlagsToMustache(fd))
		if err != nil {
			return nil, err
		}
		p := filepath.Join(root, "PropFlags.h")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			return nil, err
		}
		written = append(written, p)
	}
	type named struct {
		name string
		cls  *ir.ClassDef
	}
	var order []named
	for n, c := range unit.Classes {
		order = append(order, named{n, c})
	}
	sort.Slice(order, func(i, j int) bool {
		rank := func(k string) int {
			if k == "entity" || k == "object" {
				return 1
			}
			return 0
		}
		ri, rj := rank(order[i].cls.Kind), rank(order[j].cls.Kind)
		if ri != rj {
			return ri < rj
		}
		return order[i].name < order[j].name
	})
	for _, it := range order {
		arts, err := renderClassArtifacts(it.cls, md, legacyWire)
		if err != nil {
			return nil, err
		}
		// Drop legacy Meta fragment names if present from older emits.
		for _, obsolete := range []string{
			it.name + ".generated.inch",
			it.name + ".proxy.inch",
			it.name + ".generated.incpp",
		} {
			_ = os.Remove(filepath.Join(root, obsolete))
		}
		names := make([]string, 0, len(arts))
		for n := range arts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			p := filepath.Join(root, n)
			if err := os.WriteFile(p, []byte(arts[n]), 0o644); err != nil {
				return nil, err
			}
			written = append(written, p)
		}
	}
	return written, nil
}

// NormalizeCppSemantic strips blank lines / trailing space for golden compare.
func NormalizeCppSemantic(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		lines = append(lines, s)
	}
	return strings.Join(lines, "\n")
}
