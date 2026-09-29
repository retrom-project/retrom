package runtimeoptions

import (
	"errors"
	"reflect"
	"testing"

	runtimebundle "retrom/internal/runtime/bundle"
	runtimecatalog "retrom/internal/runtime/catalog"
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

func TestArcadeOptionsSelectMachineOnlyFromValidatedDependencySnapshot(t *testing.T) {
	schema := runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"machine": map[string]any{"type": "string", "minLength": 1, "maxLength": 32}},
		"required":   []any{"machine"},
	}
	snapshot := `{"schemaVersion":1,"kind":"ARCADE","machine":"mspacman","datVersionId":"dat-v1","closure":[],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`
	got, err := Build(runtimecatalog.OptionsArcade, schema, Input{ContentKind: "SINGLE_FILE", DependencySnapshot: snapshot})
	if err != nil || got["machine"] != "mspacman" {
		t.Fatalf("arcade options: %#v %v", got, err)
	}
	for _, invalid := range []string{
		`{"kind":"ARCADE","machine":"mspacman"}`,
		`{"schemaVersion":1,"kind":"ARCADE","machine":"../escape","datVersionId":"dat-v1","closure":[],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`,
	} {
		if _, err := Build(runtimecatalog.OptionsArcade, schema, Input{ContentKind: "SINGLE_FILE", DependencySnapshot: invalid}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid Arcade snapshot accepted: %s %v", invalid, err)
		}
	}
}
