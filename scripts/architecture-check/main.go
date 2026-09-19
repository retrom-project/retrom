// Command architecture-check inventories typed sources and explicit ownership.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"retrom/internal/testkit/architecture"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("architecture-check", flag.ContinueOnError)
	root := flags.String("root", ".", "repository root")
	mode := flags.String("mode", "inventory", "analysis mode")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if (*mode != "inventory" && *mode != "check" && *mode != "contract") || flags.NArg() != 0 {
		log.Print("architecture-check: unsupported mode or arguments")
		return 2
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		log.Print("architecture-check: invalid repository root")
		return 2
	}
	report, err := architecture.InspectRepository(ctx, absolute)
	if err != nil {
		log.Print(strings.ReplaceAll(err.Error(), absolute, "<repository>"))
		return 2
	}
	if *mode == "contract" {
		return runContractChecks(ctx, absolute, report)
	}
	if *mode == "check" {
		return runBoundaryChecks(ctx, absolute, report)
	}
	if err := architecture.WriteInventory(os.Stdout, report); err != nil {
		log.Print(err)
		return 2
	}
	if len(report.Violations) > 0 {
		return 1
	}
	return 0
}

func runBoundaryChecks(ctx context.Context, root string, inventory architecture.InventoryReport) int {
	report, err := architecture.InspectBoundaries(ctx, root, inventory)
	if err != nil {
		log.Print(strings.ReplaceAll(err.Error(), root, "<repository>"))
		return 2
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		log.Print(err)
		return 2
	}
	return architecture.BoundaryExitCode(report)
}

func runContractChecks(ctx context.Context, root string, inventory architecture.InventoryReport) int {
	report, err := architecture.InspectContracts(ctx, root, inventory)
	if err != nil {
		log.Print(strings.ReplaceAll(err.Error(), root, "<repository>"))
		return 2
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		log.Print(err)
		return 2
	}
	if report.Status != "VERIFIED" || len(report.Pending) > 0 || len(report.Violations) > 0 {
		return 1
	}
	return 0
}
