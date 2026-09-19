// Command architecture-check inventories typed sources and explicit ownership.
package main

import (
	"context"
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
	if *mode != "inventory" || flags.NArg() != 0 {
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
	if err := architecture.WriteInventory(os.Stdout, report); err != nil {
		log.Print(err)
		return 2
	}
	if len(report.Violations) > 0 {
		return 1
	}
	return 0
}
