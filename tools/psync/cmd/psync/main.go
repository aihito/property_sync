package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"property_sync/psync/internal/compile"
	"property_sync/psync/internal/emit"
	"property_sync/psync/internal/validate"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "version", "--version":
		fmt.Printf("psync %s\n", version)
	case "check":
		err = cmdCheck(args)
	case "dump":
		err = cmdDump(args)
	case "compile":
		err = cmdCompile(args)
	case "emit":
		err = cmdEmit(args)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		if ce, ok := err.(*compile.Error); ok {
			for _, d := range ce.Diagnostics {
				fmt.Fprintln(os.Stderr, d.String())
			}
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `psync %s — .psync DSL → IR / schema / lua / proto / C++

Usage:
  psync check   <entry.psync> [--root DIR]
  psync dump    <entry.psync> [--root DIR] [--allow-errors]
  psync compile <entry.psync> -o DIR [--root DIR] [--no-bundle] [--allow-errors]
  psync emit    <entry.psync> -o DIR [--root DIR] [--emitters LIST] [--native-wire] [--no-runtime] [--allow-errors]
`, version)
}

func parseCommon(args []string) (entry, root string, allowErrors bool, rest []string) {
	fs := flag.NewFlagSet("psync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&root, "root", "", "path root for relativizing entry/source_file")
	fs.BoolVar(&allowErrors, "allow-errors", false, "continue despite validation errors")
	_ = fs.Parse(args)
	rest = fs.Args()
	if len(rest) < 1 {
		return "", root, allowErrors, rest
	}
	return rest[0], root, allowErrors, rest[1:]
}

func cmdCheck(args []string) error {
	entry, root, _, _ := parseCommon(args)
	if entry == "" {
		return fmt.Errorf("entry required")
	}
	_, diags, err := compile.CompileFile(entry, true, root)
	printDiags(diags)
	if err != nil {
		return err
	}
	fmt.Printf("ok: %s\n", entry)
	return nil
}

func cmdDump(args []string) error {
	entry, root, allow, _ := parseCommon(args)
	if entry == "" {
		return fmt.Errorf("entry required")
	}
	unit, diags, err := compile.CompileFile(entry, !allow, root)
	printDiags(diags)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(unit.ToBundleIR())
}

func cmdCompile(args []string) error {
	entry, outDir, root := "", "", ""
	noBundle, allow, verbose := false, false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--output":
			i++; if i >= len(args) { return fmt.Errorf("-o needs value") }; outDir = args[i]
		case a == "--root":
			i++; if i >= len(args) { return fmt.Errorf("--root needs value") }; root = args[i]
		case a == "--no-bundle":
			noBundle = true
		case a == "--allow-errors":
			allow = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %s", a)
		default:
			if entry == "" {
				entry = a
			} else {
				return fmt.Errorf("unexpected arg %s", a)
			}
		}
	}
	if entry == "" {
		return fmt.Errorf("entry required")
	}
	if outDir == "" {
		return fmt.Errorf("-o required")
	}
	unit, diags, err := compile.CompileFile(entry, !allow, root)
	printDiags(diags)
	if err != nil {
		return err
	}
	written, err := compile.WriteIR(unit, outDir, !noBundle)
	if err != nil {
		return err
	}
	if verbose {
		for _, p := range written {
			fmt.Println(p)
		}
	} else {
		fmt.Printf("wrote %d file(s) → %s\n", len(written), outDir)
	}
	return nil
}

func cmdEmit(args []string) error {
	entry, outDir, root := "", "", ""
	emitters, nativeWire, noRuntime, allow, verbose := "", false, false, false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--output":
			i++; if i >= len(args) { return fmt.Errorf("-o needs value") }; outDir = args[i]
		case strings.HasPrefix(a, "-o="):
			outDir = strings.TrimPrefix(a, "-o=")
		case a == "--root":
			i++; if i >= len(args) { return fmt.Errorf("--root needs value") }; root = args[i]
		case strings.HasPrefix(a, "--root="):
			root = strings.TrimPrefix(a, "--root=")
		case a == "--emitters":
			i++; if i >= len(args) { return fmt.Errorf("--emitters needs value") }; emitters = args[i]
		case strings.HasPrefix(a, "--emitters="):
			emitters = strings.TrimPrefix(a, "--emitters=")
		case a == "--native-wire":
			nativeWire = true
		case a == "--no-runtime":
			noRuntime = true
		case a == "--allow-errors":
			allow = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %s", a)
		default:
			if entry == "" {
				entry = a
			} else {
				return fmt.Errorf("unexpected arg %s", a)
			}
		}
	}
	if entry == "" {
		return fmt.Errorf("entry required")
	}
	if outDir == "" {
		return fmt.Errorf("-o required")
	}
	unit, diags, err := compile.CompileFile(entry, !allow, root)
	printDiags(diags)
	if err != nil {
		return err
	}
	var list []string
	if emitters != "" {
		list = strings.Split(emitters, ",")
	}
	mustacheDir := ""
	if root != "" {
		mustacheDir = filepath.Join(root, "meta", "mustache")
	}
	opts := emit.Options{
		LegacyWire:  !nativeWire,
		CopyRuntime: !noRuntime,
		Emitters:    list,
		MustacheDir: mustacheDir,
		Flat:        false, // C++ under out/cpp/ alongside schema|lua|proto
	}
	written, err := emit.WriteAll(unit, outDir, opts)
	if err != nil {
		return err
	}
	if verbose {
		for _, p := range written {
			fmt.Println(p)
		}
	} else {
		fmt.Printf("emitted %d file(s) → %s/{schema,lua,proto,cpp}\n", len(written), outDir)
	}
	return nil
}

func printDiags(diags []validate.Diagnostic) {
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d.String())
	}
}
