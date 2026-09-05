// Package runtimeoptions owns bounded Host launch-option assembly strategies.
// Provider declarations remain the authority for validation of the final value.
package runtimeoptions

import (
	"errors"
	"fmt"

	"retrom/internal/kirikiri/detector"
	onsdetection "retrom/internal/ons/detector"
	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
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
	runtimecatalog.OptionsNone:     {[]string{}, emptyOptions},
	runtimecatalog.OptionsEmulator: {[]string{"dosEntryPath", "initialDiscIndex"}, emulatorOptions},
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
