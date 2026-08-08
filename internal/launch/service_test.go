package launch

import (
	"testing"

	"retrom/internal/dependencies"
)

func TestArtifactCompatibilityV2Validation(t *testing.T) {
	t.Parallel()
	kind := "CORE_SAVE"
	valid := artifactCompatibility{
		SchemaVersion:             2,
		RuntimeCoreID:             "future_core",
		RequestedArtifactBasename: "future_core-thread-wasm.data",
		CanvasResizePolicy:        "NONE",
		DefaultOptions:            map[string]string{},
		PersistentSaveMode:        "SINGLE_FILE",
		PersistentSaveKind:        &kind,
		InputMode:                 "STANDARD",
		StartupActions:            []dependencies.StartupAction{},
	}
	if !validArtifactCompatibility(valid) {
		t.Fatal("generic future core compatibility was rejected")
	}
	tests := map[string]func(*artifactCompatibility){
		"runtime core": func(value *artifactCompatibility) { value.RuntimeCoreID = "future-core" },
		"basename":     func(value *artifactCompatibility) { value.RequestedArtifactBasename = "../core-wasm.data" },
		"save mode":    func(value *artifactCompatibility) { value.PersistentSaveMode = "NONE" },
		"input mode":   func(value *artifactCompatibility) { value.InputMode = "MOUSE" },
		"option":       func(value *artifactCompatibility) { value.DefaultOptions = map[string]string{"__proto__": "x"} },
		"action": func(value *artifactCompatibility) {
			value.StartupActions = []dependencies.StartupAction{{
				Event: "GAME_START", Kind: "PRESS_CONTROL", DelayMS: 10_001, DurationMS: 120,
			}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := valid
			value.DefaultOptions = map[string]string{}
			value.StartupActions = []dependencies.StartupAction{}
			mutate(&value)
			if validArtifactCompatibility(value) {
				t.Fatal("malformed compatibility was accepted")
			}
		})
	}
}
