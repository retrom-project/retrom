package runtimeoptions

import (
	"errors"
	"reflect"
	"testing"

	runtimebundle "retrom/internal/runtime/bundle"
)

func TestSymbianOptionsAreExplicitAndBoundedByProvider(t *testing.T) {
	schema := runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"uid":        map[string]any{"type": "integer", "minimum": 0, "maximum": 4294967295},
			"rotation":   map[string]any{"type": "string", "enum": []any{"0", "180", "270", "90"}},
			"confirmKey": map[string]any{"type": "string", "enum": []any{"CENTER", "ENTER", "NUM5"}},
		},
		"required": []any{"confirmKey", "rotation", "uid"},
	}
	want := map[string]any{"uid": int64(0), "rotation": "0", "confirmKey": "ENTER"}
	got, err := Build("SYMBIAN_KEYPAD", schema, Input{ContentKind: "SINGLE_FILE"})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("options=%#v error=%v", got, err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties missing")
	}
	properties["confirmKey"] = map[string]any{"type": "string", "enum": []any{"CENTER"}}
	if _, err := Build("SYMBIAN_KEYPAD", schema, Input{ContentKind: "SINGLE_FILE"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported provider default: %v", err)
	}
}
