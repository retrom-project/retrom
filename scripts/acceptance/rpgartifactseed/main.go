package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var acceptanceErrorCode = regexp.MustCompile(`ACC_RPG_012_[A-Z0-9_]+`)

func main() {
	if err := runCLI(context.Background(), os.Args[1:], time.Now, os.Stdout); err != nil {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{
			"schemaVersion": stateVersion, "caseId": caseID, "status": "ERROR", "errorCode": workflowErrorCode(err),
		})
		os.Exit(1)
	}
}

func workflowErrorCode(err error) string {
	if code := acceptanceErrorCode.FindString(err.Error()); code != "" {
		return code
	}
	return "ACC_RPG_012_WORKFLOW_FAILED"
}

func runCLI(ctx context.Context, arguments []string, now clock, output *os.File) error {
	if len(arguments) == 0 {
		return errors.New("usage: rpgartifactseed prepare|promote|seed-drift|inspect [flags]")
	}
	var state seedState
	var err error
	switch arguments[0] {
	case "prepare":
		state, err = runPrepare(ctx, arguments[1:], now)
	case "promote":
		state, err = runPromote(ctx, arguments[1:], now)
	case "seed-drift":
		state, err = runSeedDrift(ctx, arguments[1:], now)
	case "inspect":
		state, err = runInspect(ctx, arguments[1:], now)
	default:
		return errors.New("usage: rpgartifactseed prepare|promote|seed-drift|inspect [flags]")
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(state)
}

func runPrepare(ctx context.Context, arguments []string, now clock) (seedState, error) {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	options := prepareOptions{}
	versionsValue := "4.2.3,4.3.0-pre"
	flags.StringVar(&options.DatabasePath, "database", "", "fresh acceptance database")
	flags.StringVar(&options.StatePath, "state", "", "acceptance state JSON")
	flags.StringVar(&options.DependencyRoot, "dependency-root", "", "byte-verified dependency root")
	flags.StringVar(&versionsValue, "versions", versionsValue, "comma-separated dependency versions")
	flags.StringVar(&options.ActiveVersion, "active", "4.2.3", "active EmulatorJS version")
	flags.StringVar(&options.CoreID, "core-id", "", "RPG Maker core ID")
	flags.StringVar(&options.OldRoute, "old-route", "", "historical route key")
	flags.StringVar(&options.NewRoute, "new-route", "", "current route key")
	flags.StringVar(&options.Acknowledgment, "acknowledge-fresh-database", "", "must equal ACC-RPG-012")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return seedState{}, errors.New("ACC_RPG_012_PREPARE_FLAGS_INVALID")
	}
	versions, err := splitVersions(versionsValue)
	if err != nil {
		return seedState{}, err
	}
	options.Versions = versions
	if !filepath.IsAbs(options.DependencyRoot) {
		return seedState{}, errors.New("ACC_RPG_012_DEPENDENCY_ROOT_MUST_BE_ABSOLUTE")
	}
	return prepare(ctx, options, now)
}

func runPromote(ctx context.Context, arguments []string, now clock) (seedState, error) {
	flags := flag.NewFlagSet("promote", flag.ContinueOnError)
	database, statePath, gameID, saveID := commonBindingFlags(flags, "old")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return seedState{}, errors.New("ACC_RPG_012_PROMOTE_FLAGS_INVALID")
	}
	return promote(ctx, *database, *statePath, *gameID, *saveID, now)
}

func runSeedDrift(ctx context.Context, arguments []string, now clock) (seedState, error) {
	flags := flag.NewFlagSet("seed-drift", flag.ContinueOnError)
	database := flags.String("database", "", "fresh acceptance database")
	statePath := flags.String("state", "", "acceptance state JSON")
	gameID := flags.String("new-game-id", "", "new-artifact product game ID")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return seedState{}, errors.New("ACC_RPG_012_DRIFT_FLAGS_INVALID")
	}
	return seedDrift(ctx, *database, *statePath, *gameID, now)
}

func runInspect(ctx context.Context, arguments []string, now clock) (seedState, error) {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	database := flags.String("database", "", "fresh acceptance database")
	statePath := flags.String("state", "", "acceptance state JSON")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return seedState{}, errors.New("ACC_RPG_012_INSPECT_FLAGS_INVALID")
	}
	return inspect(ctx, *database, *statePath, now)
}

func commonBindingFlags(flags *flag.FlagSet, prefix string) (*string, *string, *string, *string) {
	database := flags.String("database", "", "fresh acceptance database")
	statePath := flags.String("state", "", "acceptance state JSON")
	gameID := flags.String(prefix+"-game-id", "", prefix+" product game ID")
	saveID := flags.String(prefix+"-save-state-id", "", prefix+" product save-state ID")
	return database, statePath, gameID, saveID
}
