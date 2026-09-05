package runtimeoptions

import (
	"errors"
	"reflect"
	"testing"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
)

func TestRegisteredEmulatorStrategyBuildsOnlyRelevantCurrentOptions(t *testing.T) {
	schema := runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"dosEntryPath":     map[string]any{"type": []any{"string", "null"}},
			"initialDiscIndex": map[string]any{"type": []any{"integer", "null"}, "minimum": 0},
		}, "required": []any{"dosEntryPath", "initialDiscIndex"},
	}
	dos := "GAME.EXE"
	for _, fixture := range []struct {
		input Input
		want  map[string]any
	}{
		{Input{ContentKind: "SINGLE_FILE", InitialDiscIndex: 9}, map[string]any{"dosEntryPath": nil, "initialDiscIndex": nil}},
		{Input{ContentKind: "DOS_BUNDLE", DOSEntry: &dos}, map[string]any{"dosEntryPath": dos, "initialDiscIndex": nil}},
		{Input{ContentKind: "MULTI_DISC", InitialDiscIndex: 2}, map[string]any{"dosEntryPath": nil, "initialDiscIndex": int64(2)}},
	} {
		got, err := Build(runtimecatalog.OptionsEmulator, schema, fixture.input)
		if err != nil || !reflect.DeepEqual(got, fixture.want) {
			t.Fatalf("options = %#v, %v; want %#v", got, err, fixture.want)
		}
	}
	if _, err := Build(runtimecatalog.OptionsEmulator, schema, Input{ContentKind: "MULTI_DISC", InitialDiscIndex: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid Provider option accepted: %v", err)
	}
}

func TestOptionsNeverInferAStrategyFromProviderPropertyNames(t *testing.T) {
	schema := runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false, "properties": map[string]any{}, "required": []any{},
	}
	if options, err := Build(runtimecatalog.OptionsNone, schema, Input{}); err != nil || len(options) != 0 {
		t.Fatalf("empty options: %#v %v", options, err)
	}
	if _, err := Build("UNKNOWN", schema, Input{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unknown strategy: %v", err)
	}
	schema["properties"] = map[string]any{"scriptEncoding": map[string]any{"type": "string"}}
	schema["required"] = []any{"scriptEncoding"}
	if _, err := Build(runtimecatalog.OptionsNone, schema, Input{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("inferred from property: %v", err)
	}
	if _, err := Build(runtimecatalog.OptionsONS, schema, Input{DependencySnapshot: "invalid"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid source evidence: %v", err)
	}
}
