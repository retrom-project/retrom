// Package runtimeoptions owns bounded Host launch-option assembly strategies.
// Provider declarations remain the authority for validation of the final value.
package runtimeoptions

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	corevalidation "retrom/internal/core/validation"

	"retrom/internal/core/kirikiri/detector"
	onsdetection "retrom/internal/core/ons/detector"
	"retrom/internal/core/scummvm"
	runtimebundle "retrom/internal/runtime/bundle"
	runtimecatalog "retrom/internal/runtime/catalog"
)

var (
	ErrUnsupported = errors.New("RUNTIME_OPTIONS_STRATEGY_UNSUPPORTED")
	ErrInvalid     = errors.New("RUNTIME_OPTIONS_INPUT_INVALID")
)

type Input struct {
	DOSEntry           *string
	ContentKind        string
	InitialDiscIndex   int64
	DependencySnapshot string
}

type strategy struct {
	keys  []string
	build func(Input) (map[string]any, error)
}

var strategies = map[string]strategy{
	runtimecatalog.OptionsScummVM: {
		[]string{"engineId", "gameId", "root", "language", "platform", "extra", "guiOptions", "filename"}, scummVMOptions,
	},
	runtimecatalog.OptionsNone:     {[]string{}, emptyOptions},
	runtimecatalog.OptionsEmulator: {[]string{"dosEntryPath", "initialDiscIndex"}, emulatorOptions},
	runtimecatalog.OptionsArcade:   {[]string{"machine"}, arcadeOptions},
	runtimecatalog.OptionsONS:      {[]string{"scriptEncoding"}, onsOptions},
	runtimecatalog.OptionsKiriKiri: {[]string{"startupXp3Path"}, kirikiriOptions},
}

// ValidateSchema rejects unsupported Host access before startup publishes HTTP.
// It does not invent defaults for new Provider properties.
func ValidateSchema(id string, schema runtimebundle.TargetOptionsSchema) error {
	selected, registered := strategies[id]
	properties, valid := schema["properties"].(map[string]any)
	if !registered || !valid || len(properties) != len(selected.keys) {
		return ErrUnsupported
	}
	for _, key := range selected.keys {
		if _, exists := properties[key]; !exists {
			return ErrUnsupported
		}
	}
	return nil
}

func Build(id string, schema runtimebundle.TargetOptionsSchema, input Input) (map[string]any, error) {
	if err := ValidateSchema(id, schema); err != nil {
		return nil, err
	}
	options, err := strategies[id].build(input)
	if err != nil {
		return nil, err
	}
	if !runtimebundle.ValidateTargetOptions(schema, options) {
		return nil, ErrInvalid
	}
	return options, nil
}

func emptyOptions(Input) (map[string]any, error) { return map[string]any{}, nil }

func emulatorOptions(input Input) (map[string]any, error) {
	var dos, disc any
	if input.DOSEntry != nil {
		dos = *input.DOSEntry
	}
	if input.ContentKind == "MULTI_DISC" {
		disc = input.InitialDiscIndex
	}
	return map[string]any{"dosEntryPath": dos, "initialDiscIndex": disc}, nil
}

var arcadeMachinePattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func arcadeOptions(input Input) (map[string]any, error) {
	if input.ContentKind != "SINGLE_FILE" {
		return nil, ErrInvalid
	}
	if _, err := corevalidation.ParseRuntimeBIOSDependencies(input.DependencySnapshot); err != nil {
		return nil, ErrInvalid
	}
	var snapshot struct {
		Kind    string `json:"kind"`
		Machine string `json:"machine"`
	}
	if err := json.Unmarshal([]byte(input.DependencySnapshot), &snapshot); err != nil ||
		snapshot.Kind != corevalidation.SnapshotKindArcade || !arcadeMachinePattern.MatchString(snapshot.Machine) {
		return nil, ErrInvalid
	}
	return map[string]any{"machine": snapshot.Machine}, nil
}

func onsOptions(input Input) (map[string]any, error) {
	profile, err := onsdetection.ParseSnapshot(input.DependencySnapshot)
	if err != nil {
		return nil, fmt.Errorf("%w: ONS snapshot", ErrInvalid)
	}
	return map[string]any{"scriptEncoding": profile.ScriptEncoding}, nil
}

func kirikiriOptions(input Input) (map[string]any, error) {
	profile, err := detector.ParseSnapshot(input.DependencySnapshot)
	if err != nil {
		return nil, fmt.Errorf("%w: KiriKiri snapshot", ErrInvalid)
	}
	var startup any
	if profile.StartupXP3Path != nil {
		startup = *profile.StartupXP3Path
	}
	return map[string]any{"startupXp3Path": startup}, nil
}

func scummVMOptions(input Input) (map[string]any, error) {
	snapshot, err := scummvm.ParseSnapshot(input.DependencySnapshot)
	if err != nil {
		return nil, ErrInvalid
	}
	candidate, err := snapshot.Selected()
	if err != nil {
		return nil, ErrInvalid
	}
	var filename any
	if value, exists := candidate.Config["filename"]; exists {
		filename = value
	}
	return map[string]any{
		"engineId": candidate.EngineID, "gameId": candidate.GameID, "root": candidate.Root,
		"language": candidate.Language, "platform": candidate.Platform,
		"extra": candidate.Extra, "guiOptions": candidate.GUIOptions, "filename": filename,
	}, nil
}
