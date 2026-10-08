package emit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"property_sync/psync/internal/compile"
	"property_sync/psync/internal/emit"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "meta", "mustache")); err != nil {
		t.Fatal(root)
	}
	return root
}

func TestEmitCppFullFiles(t *testing.T) {
	root := repoRoot(t)
	entry := filepath.Join(root, "examples", "channel_matrix", "dsl", "player.psync")
	unit, _, err := compile.CompileFile(entry, true, root)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	md := filepath.Join(root, "meta", "mustache")
	_, err = emit.WriteAll(unit, tmp, emit.Options{
		LegacyWire: true, CopyRuntime: false, Emitters: []string{"cpp"}, MustacheDir: md, Flat: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	cppDir := filepath.Join(tmp, "cpp")
	classes := []string{"Player", "Item", "Buff", "EquipItem", "LoginRecord"}
	for _, c := range classes {
		hPath := filepath.Join(cppDir, c+".h")
		cppPath := filepath.Join(cppDir, c+".cpp")
		hb, err := os.ReadFile(hPath)
		if err != nil {
			t.Fatal(err)
		}
		cb, err := os.ReadFile(cppPath)
		if err != nil {
			t.Fatal(err)
		}
		h := string(hb)
		cpp := string(cb)
		if strings.Contains(h, ".generated.inch") || strings.Contains(h, ".proxy.inch") {
			t.Errorf("%s.h still includes inch fragments", c)
		}
		if !strings.Contains(cpp, `#include "`+c+`.h"`) {
			t.Errorf("%s.cpp missing self include", c)
		}
		for _, obsolete := range []string{c + ".generated.inch", c + ".proxy.inch", c + ".generated.incpp"} {
			if _, err := os.Stat(filepath.Join(cppDir, obsolete)); err == nil {
				t.Errorf("obsolete fragment still written: %s", obsolete)
			}
		}
		inch, proxy, incpp, err := emit.RenderClassFragments(unit.Classes[c], md, true)
		if err != nil {
			t.Fatal(err)
		}
		if emit.NormalizeCppSemantic(h) == "" {
			t.Errorf("%s.h empty", c)
		}
		if !strings.Contains(emit.NormalizeCppSemantic(h), emit.NormalizeCppSemantic(inch)) {
			t.Errorf("%s.h missing property_h body", c)
		}
		if !strings.Contains(emit.NormalizeCppSemantic(h), emit.NormalizeCppSemantic(proxy)) {
			t.Errorf("%s.h missing proxy body", c)
		}
		if !strings.Contains(emit.NormalizeCppSemantic(cpp), emit.NormalizeCppSemantic(incpp)) {
			t.Errorf("%s.cpp missing property_cpp body", c)
		}
	}
}

func TestEmitCppFragmentsVsMeta(t *testing.T) {
	root := repoRoot(t)
	metaCandidates := []string{
		filepath.Join(root, "examples", "rpg_player", "generated"),
		filepath.Join(root, "examples", "channel_matrix", "generated", "cpp"),
		filepath.Join(root, "examples", "lua_record", "generated", "cpp"),
	}
	var meta string
	for _, c := range metaCandidates {
		if _, err := os.Stat(filepath.Join(c, "Player.generated.inch")); err == nil {
			meta = c
			break
		}
	}
	if meta == "" {
		// DSL examples no longer keep inch; Meta rpg_player golden is enough.
		t.Skip("no Meta golden inch")
	}
	entry := filepath.Join(root, "examples", "channel_matrix", "dsl", "player.psync")
	unit, _, err := compile.CompileFile(entry, true, root)
	if err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(root, "meta", "mustache")
	classes := []string{"Player", "Item", "Buff", "EquipItem", "LoginRecord"}
	exts := []struct {
		ext  string
		pick func(inch, proxy, incpp string) string
	}{
		{"generated.inch", func(i, _, _ string) string { return i }},
		{"proxy.inch", func(_, p, _ string) string { return p }},
		{"generated.incpp", func(_, _, c string) string { return c }},
	}
	for _, c := range classes {
		inch, proxy, incpp, err := emit.RenderClassFragments(unit.Classes[c], md, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range exts {
			wantB, err := os.ReadFile(filepath.Join(meta, c+"."+e.ext))
			if err != nil {
				wantB, err = os.ReadFile(filepath.Join(meta, "cpp", c+"."+e.ext))
				if err != nil {
					t.Fatal(err)
				}
			}
			got := emit.NormalizeCppSemantic(e.pick(inch, proxy, incpp))
			want := emit.NormalizeCppSemantic(string(wantB))
			if got != want {
				t.Errorf("%s.%s mismatch", c, e.ext)
			}
		}
	}
}
