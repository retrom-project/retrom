package providersync

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// ErrUsage reports an invalid command line.
var ErrUsage = errors.New("usage: providers sync|verify|gc [options]")

type rootList []string

func (r *rootList) String() string { return strings.Join(*r, ",") }

func (r *rootList) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// RunCLI is the single command-line entry for tooling: sync, verify or gc.
func RunCLI(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		return ErrUsage
	}
	switch arguments[0] {
	case "sync", "verify":
		return providersSync(ctx, arguments[0] == "verify", arguments[1:])
	case "gc":
		return providersGC(arguments[1:])
	default:
		return ErrUsage
	}
}

func providersSync(ctx context.Context, full bool, arguments []string) error {
	flags := flag.NewFlagSet("providers", flag.ContinueOnError)
	options := Options{Full: full}
	var mode string
	flags.StringVar(&options.Inputs, "inputs", "", "pinned runtime-inputs.json")
	flags.StringVar(&options.Root, "root", "", "Provider root receiving active.json and installed/")
	flags.StringVar(&options.Cache, "cache", "", "chunk cache directory (default <root>/chunks)")
	flags.StringVar(&options.Source, "source", "", "chunk source directory or HTTP(S) base (default: GitHub Release)")
	flags.StringVar(&mode, "mode", string(ModeTooling), "production or tooling")
	seeds := rootList{}
	flags.Var(&seeds, "seed", "directory of verified chunks to link before downloading (repeatable)")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse providers options: %w", err)
	}
	if options.Inputs == "" || options.Root == "" || flags.NArg() != 0 {
		return ErrUsage
	}
	options.Mode, options.Seeds = Mode(mode), seeds
	result, err := Sync(ctx, options)
	if err != nil {
		return fmt.Errorf("synchronize Providers: %w", err)
	}
	return writeResult(result)
}

func providersGC(arguments []string) error {
	flags := flag.NewFlagSet("providers gc", flag.ContinueOnError)
	var cache string
	var roots rootList
	flags.StringVar(&cache, "cache", "", "shared chunk cache")
	flags.Var(&roots, "root", "Provider root whose marker keeps chunks (repeatable)")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse providers gc options: %w", err)
	}
	if cache == "" || flags.NArg() != 0 {
		return ErrUsage
	}
	removed, err := CollectGarbage(cache, roots)
	if err != nil {
		return fmt.Errorf("collect chunk garbage: %w", err)
	}
	return writeResult(map[string]int{"removedChunks": removed})
}

func writeResult(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}
