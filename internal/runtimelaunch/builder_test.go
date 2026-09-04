package runtimelaunch

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
)

func TestBuilderProducesClosedEnvelopeFromActiveProviderTarget(t *testing.T) {
	builder, binding := fixtureBuilder(t)
	contents, err := builder.Build(Input{
		Binding: binding,
		Session: Session{
			ID: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", Purpose: "PRODUCT", Mode: "SINGLE",
			Title: "Fixture", PlatformName: "Fixture", CoreName: "Fixture Core",
			ReturnTo: "/games/fixture", Warnings: []string{},
		},
		Resources: []map[string]any{{
			"kind": "ROM_BLOB", "ordinal": 0, "rangeRequired": false,
			"role": "game", "sha256": digest("e"), "sizeBytes": 3, "url": "/runtime/content/game",
		}},
		TargetOptions: map[string]any{},
		Restore: map[string]any{
			"format": "fixture-state-v1", "sha256": digest("f"),
			"sizeBytes": 3, "url": "/runtime/checkpoints/fixture",
		},
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
	base := Input{
		Binding: binding,
		Session: Session{
			ID: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", Purpose: "PRODUCT", Mode: "SINGLE",
			Title: "Fixture", PlatformName: "Fixture", CoreName: "Fixture Core",
			ReturnTo: "/games/fixture", Warnings: []string{},
		},
		Resources: []map[string]any{{
			"kind": "ROM_BLOB", "ordinal": 0, "rangeRequired": false,
			"role": "game", "sha256": digest("e"), "sizeBytes": 3, "url": "/runtime/content/game",
		}},
		TargetOptions: map[string]any{"kind": "NONE"},
	}
	for name, mutate := range map[string]func(*Input){
		"unknown target":      func(value *Input) { value.Binding.TargetID = "other" },
		"wrong resource kind": func(value *Input) { value.Resources[0]["kind"] = "FILE_TREE" },
		"missing resource":    func(value *Input) { value.Resources = nil },
		"wrong options":       func(value *Input) { value.TargetOptions["undeclared"] = true },
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

func fixtureBuilder(t *testing.T) (*Builder, runtimecatalog.Binding) {
	t.Helper()
	checkpoint := &runtimebundle.Checkpoint{WriteFormat: "fixture-state-v1", ReadFormats: []string{"fixture-state-v1"}, MaxBytes: 1024}
	target := runtimebundle.Target{
		ID: "target", DisplayName: "Fixture", GameCompatibilityLine: "fixture-v1",
		TargetOptionsSchema: runtimebundle.TargetOptionsSchema{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{}, "required": []any{},
		},
		Inputs:       []runtimebundle.Input{{Role: "game", Kind: "ROM_BLOB", Cardinality: "ONE"}},
		Capabilities: runtimebundle.Capabilities{Checkpoint: true, FrameMode: "NONE", VideoModes: []string{}, ValidationProbes: []string{}},
		Checkpoint:   checkpoint, AssetPaths: []string{"client.mjs"}, ContractSHA256: digest("d"),
	}
	manifest := runtimebundle.Manifest{
		SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: "1.0.0",
		ProviderAPI: 1, ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{target},
	}
	active := runtimebundle.ActiveDescriptor{
		SchemaVersion: 1, Source: "candidate", SourceTreeSHA256: stringPointer(digest("9")),
		Providers: []runtimebundle.ActiveProvider{{
			ProviderID: "fixture", ProviderVersion: "1.0.0", ProviderAPI: 1,
			BundleSHA256: digest("a"), ModuleSHA256: digest("b"), ClientModulePath: "client.mjs",
			Targets: []runtimebundle.ActiveTarget{{
				ID: "target", GameCompatibilityLine: "fixture-v1",
				Checkpoint: checkpoint, ContractSHA256: digest("d"),
			}},
		}},
	}
	builder, err := NewBuilder(active, map[string]runtimebundle.Manifest{"fixture": manifest})
	if err != nil {
		t.Fatal(err)
	}
	return builder, runtimecatalog.Binding{ProviderID: "fixture", TargetID: "target", LaunchPolicy: "SUPPORTED"}
}

func cloneInput(value Input) Input {
	result := value
	result.Resources = make([]map[string]any, len(value.Resources))
	for index, resource := range value.Resources {
		result.Resources[index] = make(map[string]any, len(resource))
		for key, item := range resource {
			result.Resources[index][key] = item
		}
	}
	result.TargetOptions = make(map[string]any, len(value.TargetOptions))
	for key, item := range value.TargetOptions {
		result.TargetOptions[key] = item
	}
	return result
}

func digest(value string) string         { return strings.Repeat(value, 64) }
func stringPointer(value string) *string { return &value }
