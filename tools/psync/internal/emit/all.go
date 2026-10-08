package emit

import (
	"strings"

	"property_sync/psync/internal/ir"
)

func writeAll(unit *ir.CompilationUnit, outDir string, opts Options) ([]string, error) {
	emitters := opts.Emitters
	if len(emitters) == 0 {
		emitters = []string{"schema", "lua", "proto", "cpp"}
	}
	want := map[string]bool{}
	for _, e := range emitters {
		want[strings.TrimSpace(e)] = true
	}

	var written []string
	if want["schema"] || want["lua"] || want["proto"] {
		ws, err := writeSchemaLuaProto(unit, outDir, opts.LegacyWire, opts.CopyRuntime && want["lua"], opts.RuntimeDir)
		if err != nil {
			return nil, err
		}
		written = append(written, ws...)
	}
	if want["cpp"] {
		// Flat=true → files directly under outDir; Flat=false → outDir/cpp/
		md := mustacheDir(opts.MustacheDir)
		ws, err := writeCpp(unit, outDir, md, opts.LegacyWire, opts.Flat)
		if err != nil {
			return nil, err
		}
		written = append(written, ws...)
	}
	return written, nil
}
