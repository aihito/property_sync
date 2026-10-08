// Package emit writes schema/lua/proto/C++ from IR.
package emit

import (
	"property_sync/psync/internal/ir"
)

type Options struct {
	LegacyWire  bool
	CopyRuntime bool
	Emitters    []string // empty = all
	MustacheDir string
	RuntimeDir  string
	Flat        bool
}

// WriteAll is implemented in emit_all.go / language-specific files.
func WriteAll(unit *ir.CompilationUnit, outDir string, opts Options) ([]string, error) {
	return writeAll(unit, outDir, opts)
}
