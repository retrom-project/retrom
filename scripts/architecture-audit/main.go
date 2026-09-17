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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"retrom/internal/testkit/architecture"
)

func main() {
	root := flag.String("root", ".", "repository root directory")
	mode := flag.String("mode", "check", "inventory or check")
	output := flag.String("output", "", "JSON output path (default stdout)")
	scope := flag.String("scope", "", "package path scope for development checks (unused in final)")
	flag.Parse()
	_ = scope

	if *mode != "inventory" && *mode != "check" {
		fmt.Fprintf(os.Stderr, "invalid mode %q: must be inventory or check\n", *mode)
		os.Exit(2)
	}

	result, err := architecture.ScanDirectory(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
		os.Exit(2)
	}

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "analysis error: %s\n", e)
		}
	}

	report := map[string]interface{}{
		"mode":       *mode,
		"violations": result.Violations,
		"errors":     result.Errors,
		"summary": map[string]int{
			"violations": len(result.Violations),
			"errors":     len(result.Errors),
		},
	}
	if *mode == "inventory" {
		report["inventory"] = result.Inventory
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		os.Exit(2)
	}

	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir error: %v\n", err)
			os.Exit(2)
		}
		if err := os.WriteFile(*output, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write error: %v\n", err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d violations, %d errors)\n", *output, len(result.Violations), len(result.Errors))
	} else {
		os.Stdout.Write(data)       //nolint:errcheck // stdout write failure exits naturally
		os.Stdout.WriteString("\n") //nolint:errcheck // stdout newline
	}

	if *mode == "check" && len(result.Violations) > 0 {
		for _, v := range result.Violations {
			fmt.Fprintf(os.Stderr, "[%s] %s:%d %s\n", v.Rule, v.File, v.Line, v.Message)
		}
		os.Exit(1)
	}
}
