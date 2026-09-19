package runtimeoptions

import (
	json "encoding/json"
	"errors"
	"reflect"
	"testing"

	"retrom/internal/capability/runtime/runtimecatalog"
	"retrom/internal/capability/runtime/runtimejson"
	runtimecontract "retrom/internal/model/runtimecontract"
)

func TestRegisteredEmulatorStrategyBuildsOnlyRelevantCurrentOptions(t *testing.T) {
	schema := runtimecontract.TargetOptionsSchema{
		"type": json.RawMessage("\"object\""), "additionalProperties": json.RawMessage("false"),
		"properties": json.RawMessage("{\"dosEntryPath\":{\"type\":[\"string\",\"null\"]},\"initialDiscIndex\":{\"minimum\":0,\"type\":[\"integer\",\"null\"]}}"),

		"required": json.RawMessage("[\"dosEntryPath\",\"initialDiscIndex\"]"),
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
		if err != nil {
			t.Fatal(err)
		}
		decoded, parseErr := runtimejson.ParseStrictJSON(got)
		if parseErr != nil || !reflect.DeepEqual(decoded, fixture.want) {
			t.Fatalf("options = %#v, %v; want %#v", got, err, fixture.want)
		}
	}
	if _, err := Build(runtimecatalog.OptionsEmulator, schema, Input{ContentKind: "MULTI_DISC", InitialDiscIndex: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid Provider option accepted: %v", err)
	}
}

func TestOptionsNeverInferAStrategyFromProviderPropertyNames(t *testing.T) {
	schema := runtimecontract.TargetOptionsSchema{
		"type": json.RawMessage("\"object\""), "additionalProperties": json.RawMessage("false"), "properties": json.RawMessage("{}"), "required": json.RawMessage("[]"),
	}
	if options, err := Build(runtimecatalog.OptionsNone, schema, Input{}); err != nil || string(options) != "{}" {
		t.Fatalf("empty options: %#v %v", options, err)
	}
	if _, err := Build("UNKNOWN", schema, Input{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unknown strategy: %v", err)
	}
	schema["properties"] = json.RawMessage(`{"scriptEncoding":{"type":"string"}}`)
	schema["required"] = json.RawMessage(`["scriptEncoding"]`)
	if _, err := Build(runtimecatalog.OptionsNone, schema, Input{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("inferred from property: %v", err)
	}
	if _, err := Build(runtimecatalog.OptionsONS, schema, Input{DependencySnapshot: "invalid"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid source evidence: %v", err)
	}
}
