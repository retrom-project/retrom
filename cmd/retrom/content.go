package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/service/content"
	"retrom/internal/storage"
)

type contentOptions struct {
	apply bool
	all   bool
	id    string
	limit int
}

func parseContentOptions(args []string) (contentOptions, error) {
	options := contentOptions{}
	if len(args) == 0 || args[0] != "pack-existing" {
		return options, fmt.Errorf("content pack-existing requires --all or --game-id: %w", model.ErrInvalid)
	}
	flags := flag.NewFlagSet("pack-existing", flag.ContinueOnError)
	flags.BoolVar(&options.apply, "apply", false, "atomically switch verified games to STORE ZIP")
	dry := flags.Bool("dry-run", false, "preview without writing content")
	flags.BoolVar(&options.all, "all", false, "consider pending and published games")
	flags.StringVar(&options.id, "game-id", "", "convert one game")
	flags.IntVar(&options.limit, "limit", 20, "maximum project games (1..1000)")
	if err := flags.Parse(args[1:]); err != nil {
		return options, fmt.Errorf("parse content options: %w", err)
	}
	if flags.NArg() != 0 || options.apply && *dry || options.all == (options.id != "") {
		return options, model.ErrInvalid
	}
	if options.id != "" && !model.UUID(options.id) || options.limit < 1 || options.limit > 1000 {
		return options, model.ErrInvalid
	}
	return options, nil
}

func contentCLI(ctx context.Context, args []string) error {
	options, err := parseContentOptions(args)
	if err != nil {
		return err
	}
	repo, err := persistence.Open(ctx, os.Getenv("RETROM_DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open conversion database: %w", err)
	}
	defer repo.Close()
	managed, err := storage.Open(environment("RETROM_DATA_DIR", ".pfb/workspace/data") + "/managed")
	if err != nil {
		return fmt.Errorf("open managed content: %w", err)
	}
	defer func() {
		if closeErr := managed.Close(); closeErr != nil {
			slog.Error("close managed content", "error", closeErr)
		}
	}()
	runtime, err := runtimeclient.Open(ctx, environment("RETROM_RUNTIME_ROOT", "../retrom-runtime"),
		environment("RETROM_NODE", "node"), environment("RETROM_PROVIDER_ROOT", ".pfb/workspace/providers"))
	if err != nil {
		return fmt.Errorf("open conversion runtime: %w", err)
	}
	defer runtime.Close()
	service := content.Service{Repository: repo, Storage: managed, Runtime: runtime, Now: time.Now}
	encoder := json.NewEncoder(os.Stdout)
	err = service.PackBatch(ctx, options.id, options.limit, options.apply, func(result content.PackResult) error {
		if err := encoder.Encode(result); err != nil {
			return fmt.Errorf("write conversion result: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("pack existing content: %w", err)
	}
	return nil
}
