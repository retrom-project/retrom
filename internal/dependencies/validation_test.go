package dependencies

import "testing"

func TestSelectedCoreStartupActionDelayBoundary(t *testing.T) {
	t.Parallel()
	kind := "CORE_SAVE"
	reportPath := "data/cores/reports/test.json"
	core := SelectedCore{
		CoreID:                    "test",
		SourceComponentID:         "test",
		RuntimeCoreID:             "test",
		LocalPath:                 "data/cores/test-wasm.data",
		BundleVersion:             "4.2.3",
		ArtifactFlavor:            "OVERRIDE",
		RequestedArtifactBasename: "test-wasm.data",
		CanvasResizePolicy:        "NONE",
		DefaultOptions:            map[string]string{},
		PersistentSaveMode:        "SINGLE_FILE",
		PersistentSaveKind:        &kind,
		InputMode:                 "STANDARD",
		StartupActions: []StartupAction{{
			Event: "GAME_START", Kind: "PRESS_CONTROL", DelayMS: 30_000,
			Player: 0, Control: 3, DurationMS: 120,
		}},
		ReportPath:            reportPath,
		SupportedContentKinds: []string{"SINGLE_FILE"},
	}
	allowlist := map[string]File{reportPath: {Path: reportPath}}
	if err := validateSelectedCore(core, allowlist, 6); err != nil {
		t.Fatalf("30 second action rejected: %v", err)
	}
	core.PersistentSaveMode = "FILE_TREE"
	if err := validateSelectedCore(core, allowlist, 6); err != nil {
		t.Fatalf("generic file tree rejected: %v", err)
	}
	if err := validateSelectedCore(core, allowlist, 5); err == nil {
		t.Fatal("legacy manifest accepted non-PPSSPP file tree")
	}
	core.PersistentSaveMode = "AUTO_STATE"
	if err := validateSelectedCore(core, allowlist, 6); err != nil {
		t.Fatalf("automatic state rejected: %v", err)
	}
	core.PersistentSaveMode = "SINGLE_FILE"
	core.StartupActions[0].DelayMS = 30_001
	if err := validateSelectedCore(core, allowlist, 6); err == nil {
		t.Fatal("30 second plus one millisecond action accepted")
	}
}
