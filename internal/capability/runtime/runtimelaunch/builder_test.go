package runtimelaunch

import (
	json "encoding/json"
	"errors"
	"strings"
	"testing"

	"retrom/internal/capability/runtime/runtimebundle"
	runtimecontract "retrom/internal/model/runtimecontract"
)

func TestBuilderProducesClosedEnvelopeFromActiveProviderTarget(t *testing.T) {
	builder, binding := fixtureBuilder(t)
	contents, err := builder.Build(runtimecontract.LaunchInput{
		Binding: binding,
		Session: runtimecontract.LaunchSession{
			ID: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", Purpose: "PRODUCT", Mode: "SINGLE",
			Title: "Fixture", PlatformName: "Fixture", CoreName: "Fixture Core",
			ReturnTo: "/games/fixture", Warnings: []string{},
		},
		Resources: []json.RawMessage{encodeLaunchFixture(t, map[string]any{
			"kind": "ROM_BLOB", "ordinal": 0, "rangeRequired": false,
			"role": "game", "sha256": digest("e"), "sizeBytes": 3, "url": "/runtime/content/game",
		})},
		TargetOptions: encodeLaunchFixture(t, map[string]any{}),
		Restore: encodeLaunchFixture(t, map[string]any{
			"format": "fixture-state-v1", "sha256": digest("f"),
			"sizeBytes": 3, "url": "/runtime/checkpoints/fixture",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := runtimebundle.ParseLaunchEnvelope(contents)
	if err != nil {
		t.Fatalf("built envelope rejected: %v\n%s", err, contents)
	}
	runtime, ok := envelope["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime = %#v", envelope["runtime"])
	}
	if runtime["providerId"] != "fixture" || runtime["targetId"] != "target" ||
		runtime["moduleUrl"] != "/runtime/providers/fixture/"+digest("a")+"/client.mjs" {
		t.Fatalf("runtime = %#v", runtime)
	}
	session, ok := envelope["session"].(map[string]any)
	if !ok || session["coreName"] != "Fixture Core" {
		t.Fatalf("session = %#v", envelope["session"])
	}
}

func TestBuilderRejectsTargetDriftAndResourceOrOptionsMismatch(t *testing.T) {
	builder, binding := fixtureBuilder(t)
	base := runtimecontract.LaunchInput{
		Binding: binding,
		Session: runtimecontract.LaunchSession{
			ID: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", Purpose: "PRODUCT", Mode: "SINGLE",
			Title: "Fixture", PlatformName: "Fixture", CoreName: "Fixture Core",
			ReturnTo: "/games/fixture", Warnings: []string{},
		},
		Resources: []json.RawMessage{encodeLaunchFixture(t, map[string]any{
			"kind": "ROM_BLOB", "ordinal": 0, "rangeRequired": false,
			"role": "game", "sha256": digest("e"), "sizeBytes": 3, "url": "/runtime/content/game",
		})},
		TargetOptions: encodeLaunchFixture(t, map[string]any{}),
	}
	if _, err := builder.Build(base); err != nil {
		t.Fatalf("valid base input: %v", err)
	}
	for name, mutate := range map[string]func(*runtimecontract.LaunchInput){
		"unknown target": func(value *runtimecontract.LaunchInput) { value.Binding.TargetID = "other" },
		"wrong resource kind": func(value *runtimecontract.LaunchInput) {
			value.Resources[0] = mutateLaunchFixture(t, value.Resources[0], "kind", "FILE_TREE")
		},
		"missing resource": func(value *runtimecontract.LaunchInput) { value.Resources = nil },
		"wrong options": func(value *runtimecontract.LaunchInput) {
			value.TargetOptions = mutateLaunchFixture(t, value.TargetOptions, "undeclared", true)
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneInput(base)
			mutate(&candidate)
			if _, err := builder.Build(candidate); !errors.Is(err, ErrEnvelopeInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func fixtureBuilder(t *testing.T) (*Builder, runtimecontract.Binding) {
	t.Helper()
	checkpoint := &runtimecontract.Checkpoint{WriteFormat: "fixture-state-v1", ReadFormats: []string{"fixture-state-v1"}, MaxBytes: 1024}
	target := runtimecontract.Target{
		ID: "target", DisplayName: "Fixture",
		TargetOptionsSchema: runtimecontract.TargetOptionsSchema{
			"type": json.RawMessage("\"object\""), "additionalProperties": json.RawMessage("false"),
			"properties": json.RawMessage("{}"), "required": json.RawMessage("[]"),
		},
		Inputs:       []runtimecontract.Input{{Role: "game", Kind: "ROM_BLOB", Cardinality: "ONE"}},
		Capabilities: runtimecontract.Capabilities{Checkpoint: true, FrameMode: "NONE", VideoModes: []string{}},
		Checkpoint:   checkpoint, AssetPaths: []string{"client.mjs"},
	}
	manifest := runtimecontract.Manifest{
		SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: "1.0.0",
		ProviderAPI: 1, ClientModulePath: "client.mjs", Targets: []runtimecontract.Target{target},
	}
	active := runtimecontract.ActiveDescriptor{
		SchemaVersion: 1, Source: "candidate", SourceTreeSHA256: stringPointer(digest("9")),
		Providers: []runtimecontract.ActiveProvider{{
			ProviderID: "fixture", ProviderVersion: "1.0.0", ProviderAPI: 1,
			BundleSHA256: digest("a"), ModuleSHA256: digest("b"), ClientModulePath: "client.mjs",
			Targets: []runtimecontract.ActiveTarget{{
				ID: "target", Checkpoint: checkpoint,
			}},
		}},
	}
	builder, err := NewBuilder(active, map[string]runtimecontract.Manifest{"fixture": manifest})
	if err != nil {
		t.Fatal(err)
	}
	return builder, runtimecontract.Binding{ProviderID: "fixture", TargetID: "target", LaunchPolicy: "SUPPORTED"}
}

func cloneInput(value runtimecontract.LaunchInput) runtimecontract.LaunchInput {
	result := value
	result.Resources = make([]json.RawMessage, len(value.Resources))
	for index, resource := range value.Resources {
		result.Resources[index] = append(json.RawMessage(nil), resource...)
	}
	result.TargetOptions = append(json.RawMessage(nil), value.TargetOptions...)
	return result
}

func mutateLaunchFixture(t *testing.T, contents json.RawMessage, key string, value any) json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(contents, &object); err != nil {
		t.Fatal(err)
	}
	object[key] = encodeLaunchFixture(t, value)
	return encodeLaunchFixture(t, object)
}

func digest(value string) string         { return strings.Repeat(value, 64) }
func stringPointer(value string) *string { return &value }

func encodeLaunchFixture(t *testing.T, value any) json.RawMessage {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
