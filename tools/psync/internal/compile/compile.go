package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"property_sync/psync/internal/ir"
	"property_sync/psync/internal/parse"
	"property_sync/psync/internal/validate"
)

type Error struct {
	Msg          string
	Diagnostics  []validate.Diagnostic
}

func (e *Error) Error() string { return e.Msg }

func relPath(path, root string) string {
	absPath, err1 := filepath.Abs(path)
	absRoot, err2 := filepath.Abs(root)
	if err1 != nil || err2 != nil {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	return filepath.ToSlash(rel)
}

func resolveImport(fromFile, rel string) (string, error) {
	candidate := filepath.Clean(filepath.Join(filepath.Dir(fromFile), rel))
	if st, err := os.Stat(candidate); err != nil || st.IsDir() {
		return "", &Error{Msg: fmt.Sprintf("%s: import not found: %s", fromFile, rel)}
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func LoadUnit(entry string, root string) (*ir.CompilationUnit, error) {
	absEntry, err := filepath.Abs(entry)
	if err != nil {
		return nil, err
	}
	if root == "" {
		root = filepath.Dir(absEntry)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	unit := ir.NewUnit(relPath(absEntry, absRoot))
	visited := map[string]bool{}

	var loadOne func(string) error
	loadOne = func(path string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if visited[abs] {
			return nil
		}
		visited[abs] = true

		imports, flagsList, classes, err := parse.ParsePath(abs)
		if err != nil {
			return &Error{Msg: fmt.Sprintf("%s: %v", abs, err)}
		}
		for _, rel := range imports {
			imp, err := resolveImport(abs, rel)
			if err != nil {
				return err
			}
			if err := loadOne(imp); err != nil {
				return err
			}
		}
		for i := range flagsList {
			fd := flagsList[i]
			if _, ok := unit.Flags[fd.Name]; ok {
				return &Error{Msg: fmt.Sprintf("duplicate flags %s (from %s; already defined)", fd.Name, abs)}
			}
			cp := fd
			unit.Flags[fd.Name] = &cp
		}
		for i := range classes {
			cls := classes[i]
			cls.SourceFile = relPath(abs, absRoot)
			if other, ok := unit.Classes[cls.Name]; ok {
				return &Error{Msg: fmt.Sprintf("duplicate class %s in %s (already from %s)", cls.Name, abs, other.SourceFile)}
			}
			cp := cls
			unit.Classes[cls.Name] = &cp
		}
		return nil
	}

	if err := loadOne(absEntry); err != nil {
		return nil, err
	}
	return unit, nil
}

func CompileFile(entry string, strict bool, root string) (*ir.CompilationUnit, []validate.Diagnostic, error) {
	unit, err := LoadUnit(entry, root)
	if err != nil {
		return nil, nil, err
	}
	diags := validate.Validate(unit, nil)
	if strict && validate.HasErrors(diags) {
		return nil, diags, &Error{Msg: "validation failed", Diagnostics: diags}
	}
	return unit, diags, nil
}

func WriteIR(unit *ir.CompilationUnit, outDir string, bundle bool) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	writeJSON := func(name string, v any) error {
		p := filepath.Join(outDir, name)
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		b = append(b, '\n')
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
		written = append(written, p)
		return nil
	}
	for _, name := range sortedMapKeys(unit.Flags) {
		if err := writeJSON(name+".ir.json", unit.Flags[name].ToIR()); err != nil {
			return nil, err
		}
	}
	for _, name := range sortedMapKeys(unit.Classes) {
		if err := writeJSON(name+".ir.json", unit.Classes[name].ToIR()); err != nil {
			return nil, err
		}
	}
	if bundle {
		if err := writeJSON("_bundle.ir.json", unit.ToBundleIR()); err != nil {
			return nil, err
		}
	}
	return written, nil
}

func sortedMapKeys[T any](m map[string]T) []string {
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

// RepoRoot walks up from start looking for go.mod or CMakeLists at property_sync root.
func RepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "tools", "psync", "testdata", "dsl", "player.psync")); err == nil {
			return dir
		}
		if strings.HasSuffix(filepath.ToSlash(dir), "/tools/psync") {
			return filepath.Dir(filepath.Dir(dir))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}
