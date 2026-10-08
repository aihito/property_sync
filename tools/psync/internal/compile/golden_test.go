package compile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"property_sync/psync/internal/compile"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/compile → tools/psync → repo
	modRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(modRoot, "testdata", "ir")); err != nil {
		t.Fatalf("tools/psync/testdata/ir not found from %s (mod=%s)", wd, modRoot)
	}
	return filepath.Clean(filepath.Join(modRoot, "..", ".."))
}

func TestCompileMatchesGolden(t *testing.T) {
	root := repoRoot(t)
	entry := filepath.Join(root, "dsl", "player.psync")
	unit, diags, err := compile.CompileFile(entry, true, root)
	if err != nil {
		t.Fatalf("compile: %v diags=%v", err, diags)
	}
	for _, d := range diags {
		if d.Level == "error" {
			t.Fatalf("error diag: %s", d)
		}
	}
	tmp := t.TempDir()
	if _, err := compile.WriteIR(unit, tmp, false); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(root, "tools", "psync", "testdata", "ir")
	entries, err := os.ReadDir(golden)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		wantB, err := os.ReadFile(filepath.Join(golden, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		gotB, err := os.ReadFile(filepath.Join(tmp, e.Name()))
		if err != nil {
			t.Fatalf("missing %s: %v", e.Name(), err)
		}
		var want, got any
		if err := json.Unmarshal(wantB, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(gotB, &got); err != nil {
			t.Fatal(err)
		}
		wb, _ := json.Marshal(want)
		gb, _ := json.Marshal(got)
		if string(wb) != string(gb) {
			t.Errorf("%s mismatch\n got=%s\nwant=%s", e.Name(), gb, wb)
		}
	}
}

func TestCheckPlayer(t *testing.T) {
	root := repoRoot(t)
	entry := filepath.Join(root, "dsl", "player.psync")
	_, diags, err := compile.CompileFile(entry, true, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if d.Level == "error" {
			t.Fatal(d)
		}
	}
}
