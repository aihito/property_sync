package emit_test

import (
	"os"
	"path/filepath"
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

func TestEmitCppVsMeta(t *testing.T) {
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
		t.Skip("no Meta golden inch")
	}
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
	exts := []string{"generated.inch", "proxy.inch", "generated.incpp"}
	for _, c := range classes {
		for _, ext := range exts {
			gotB, err := os.ReadFile(filepath.Join(cppDir, c+"."+ext))
			if err != nil {
				t.Fatal(err)
			}
			wantB, err := os.ReadFile(filepath.Join(meta, c+"."+ext))
			if err != nil {
				// Meta golden may still be flat under generated/ or under generated/cpp/
				wantB, err = os.ReadFile(filepath.Join(meta, "cpp", c+"."+ext))
				if err != nil {
					t.Fatal(err)
				}
			}
			got := emit.NormalizeCppSemantic(string(gotB))
			want := emit.NormalizeCppSemantic(string(wantB))
			if got != want {
				t.Errorf("%s.%s mismatch", c, ext)
			}
		}
	}
}
