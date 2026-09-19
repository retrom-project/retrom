// architecture-audit scans the Retrom backend for layering violations.
//
// Usage:
//
//	go run ./scripts/architecture-audit -root . -mode=inventory -output report.json
//	go run ./scripts/architecture-audit -root . -mode=check -output report.json
//
// Exit codes: 0 = success, 1 = violations found, 2 = internal error.
package main

import (
	"flag"
	"os"
)

func main() {
	root := flag.String("root", ".", "repository root directory")
	mode := flag.String("mode", "check", "inventory or check")
	output := flag.String("output", "", "JSON output path (default stdout)")
	scope := flag.String("scope", "", "package path scope for development checks (unused in final)")
	flag.Parse()
	_ = scope

	os.Exit(Run(*root, *mode, *output, os.Stdout, os.Stderr))
}
