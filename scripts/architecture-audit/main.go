// architecture-audit scans the Retrom backend for layering violations.
//
// Usage:
//
//	go run ./scripts/architecture-audit -root . -mode=inventory -output report.json
//	go run ./scripts/architecture-audit -root . -mode=check -output report.json
//	go run ./scripts/architecture-audit -root . -mode=check -fail-on=LAYER-001,LAYER-002,LAYER-006,LAYER-007
//
// Exit codes: 0 = success, 1 = violations found, 2 = internal error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"retrom/internal/testkit/architecture"
)

func main() {
	root := flag.String("root", ".", "repository root directory")
	mode := flag.String("mode", "check", "inventory or check")
	output := flag.String("output", "", "JSON output path (default stdout)")
	scope := flag.String("scope", "", "package path scope for development checks (unused in final)")
	failOn := flag.String("fail-on", "", "comma-separated rules that cause exit 1; empty = all rules")
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

	report := buildReport(*mode, result)
	if err := writeReport(report, *output, result); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "analysis error: %s\n", e)
		}
		os.Exit(2)
	}

	if *mode == "check" {
		exitOnViolations(result.Violations, *failOn)
	}
}

func buildReport(mode string, result *architecture.ScanResult) map[string]interface{} {
	report := map[string]interface{}{
		"mode":       mode,
		"violations": result.Violations,
		"errors":     result.Errors,
		"summary": map[string]int{
			"violations": len(result.Violations),
			"errors":     len(result.Errors),
		},
	}
	if mode == "inventory" {
		report["inventory"] = result.Inventory
	}
	return report
}

func writeReport(report map[string]interface{}, output string, result *architecture.ScanResult) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	if output != "" {
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return fmt.Errorf("mkdir error: %w", err)
		}
		if err := os.WriteFile(output, data, 0o644); err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d violations, %d errors)\n",
			output, len(result.Violations), len(result.Errors))
		return nil
	}
	os.Stdout.Write(data)       //nolint:errcheck // stdout write failure exits naturally
	os.Stdout.WriteString("\n") //nolint:errcheck // stdout newline
	return nil
}

func exitOnViolations(violations []architecture.Violation, failOnStr string) {
	if len(violations) == 0 {
		return
	}
	failRules := parseFailOn(failOnStr)
	blocking := filterBlocking(violations, failRules)
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "[%s] %s:%d %s\n", v.Rule, v.File, v.Line, v.Message)
	}
	if len(blocking) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d blocking violation(s) out of %d total\n",
			len(blocking), len(violations))
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\n%d violation(s) reported, 0 blocking\n",
		len(violations))
}

func parseFailOn(value string) map[string]bool {
	if value == "" {
		return nil
	}
	rules := make(map[string]bool)
	for _, r := range strings.Split(value, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			rules[r] = true
		}
	}
	return rules
}

func filterBlocking(
	violations []architecture.Violation, failRules map[string]bool,
) []architecture.Violation {
	if failRules == nil {
		return violations
	}
	var blocking []architecture.Violation
	for _, v := range violations {
		if failRules[v.Rule] {
			blocking = append(blocking, v)
		}
	}
	return blocking
}
