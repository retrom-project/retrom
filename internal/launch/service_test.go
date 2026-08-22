package launch

import (
	"testing"

	"retrom/internal/dependencies"
	"retrom/internal/testassert"
)

func TestArtifactCompatibilityValidation(t *testing.T) {
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
	testassert.True(t, validArtifactCompatibility(valid), "generic future core compatibility was rejected")
	valid.RuntimeCoreID = "ppsspp"
	valid.PersistentSaveMode = "FILE_TREE"
	testassert.True(t, validArtifactCompatibility(valid), "PPSSPP file-tree compatibility was rejected")
	valid.RuntimeCoreID = "future_core"
	testassert.False(t, validArtifactCompatibility(valid), "non-PPSSPP file-tree compatibility was accepted")
	valid.SchemaVersion = 4
	valid.SupportedContentKinds = []string{"SINGLE_FILE"}
	testassert.True(t, validArtifactCompatibility(valid), "V4 generic file-tree compatibility was rejected")
	valid.PersistentSaveMode = "AUTO_STATE"
	testassert.True(t, validArtifactCompatibility(valid), "V4 automatic-state compatibility was rejected")
	valid.PersistentSaveMode = "SINGLE_FILE"
	valid.StartupActions = []dependencies.StartupAction{{
		Event: "GAME_START", Kind: "PRESS_CONTROL", DelayMS: 30_000, DurationMS: 120,
	}}
	testassert.True(t, validArtifactCompatibility(valid), "30 second startup action was rejected")
	valid.StartupActions = []dependencies.StartupAction{}
	tests := map[string]func(*artifactCompatibility){
		"runtime core": func(value *artifactCompatibility) { value.RuntimeCoreID = "future-core" },
		"basename":     func(value *artifactCompatibility) { value.RequestedArtifactBasename = "../core-wasm.data" },
		"save mode":    func(value *artifactCompatibility) { value.PersistentSaveMode = "NONE" },
		"input mode":   func(value *artifactCompatibility) { value.InputMode = "MOUSE" },
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
