package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"retrom/internal/testkit/architecture"
)

// Run executes the architecture audit and returns the exit code.
// 0 = success, 1 = violations found, 2 = internal/analysis error.
func Run(root, mode, output string, stdout, stderr io.Writer) int {
	if mode != "inventory" && mode != "check" {
		fmt.Fprintf(stderr, "invalid mode %q: must be inventory or check\n", mode)
		return 2
	}

	result, err := architecture.ScanDirectory(root)
	if err != nil {
		fmt.Fprintf(stderr, "scan error: %v\n", err)
		return 2
	}

	report := buildAuditReport(mode, result)
	if err := writeAuditReport(report, output, result, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			fmt.Fprintf(stderr, "analysis error: %s\n", e)
		}
		return 2
	}

	if mode == "check" && len(result.Violations) > 0 {
		for _, v := range result.Violations {
			fmt.Fprintf(stderr, "[%s] %s:%d %s\n", v.Rule, v.File, v.Line, v.Message)
		}
		fmt.Fprintf(stderr, "\n%d violation(s)\n", len(result.Violations))
		return 1
	}

	return 0
}

func buildAuditReport(mode string, result *architecture.ScanResult) map[string]interface{} {
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

func writeAuditReport(report map[string]interface{}, output string, result *architecture.ScanResult, stdout, stderr io.Writer) error {
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
		fmt.Fprintf(stderr, "wrote %s (%d violations, %d errors)\n",
			output, len(result.Violations), len(result.Errors))
		return nil
	}
	if _, err := stdout.Write(data); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return fmt.Errorf("write stdout newline: %w", err)
	}
	return nil
}
