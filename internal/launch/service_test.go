package launch

import (
	"testing"

	"retrom/internal/dependencies"
	"retrom/internal/testassert"
)

func TestArtifactCompatibilityValidation(t *testing.T) {
	t.Parallel()
	valid := artifactCompatibility{
		SchemaVersion:             5,
		RuntimeCoreID:             "future_core",
		RequestedArtifactBasename: "future_core-thread-wasm.data",
		CanvasResizePolicy:        "NONE",
		DefaultOptions:            map[string]string{},
		InputMode:                 "STANDARD",
		StartupActions:            []dependencies.StartupAction{},
		SupportedContentKinds:     []string{"SINGLE_FILE"},
	}
	testassert.True(t, validArtifactCompatibility(valid), "generic future core compatibility was rejected")
	valid.StartupActions = []dependencies.StartupAction{{
		Event: "GAME_START", Kind: "PRESS_CONTROL", DelayMS: 30_000, DurationMS: 120,
	}}
	testassert.True(t, validArtifactCompatibility(valid), "30 second startup action was rejected")
	valid.StartupActions = []dependencies.StartupAction{}
	tests := map[string]func(*artifactCompatibility){
		"schema":       func(value *artifactCompatibility) { value.SchemaVersion = 4 },
		"runtime core": func(value *artifactCompatibility) { value.RuntimeCoreID = "future-core" },
		"basename":     func(value *artifactCompatibility) { value.RequestedArtifactBasename = "../core-wasm.data" },
		"input mode":   func(value *artifactCompatibility) { value.InputMode = "MOUSE" },
		"content kind": func(value *artifactCompatibility) { value.SupportedContentKinds = nil },
		"option":       func(value *artifactCompatibility) { value.DefaultOptions = map[string]string{"__proto__": "x"} },
		"action": func(value *artifactCompatibility) {
			value.StartupActions = []dependencies.StartupAction{{
				Event: "GAME_START", Kind: "PRESS_CONTROL", DelayMS: 30_001, DurationMS: 120,
			}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := valid
			value.DefaultOptions = map[string]string{}
			value.StartupActions = []dependencies.StartupAction{}
			mutate(&value)
			testassert.False(t, validArtifactCompatibility(value), "malformed compatibility was accepted")
		})
	}
}
