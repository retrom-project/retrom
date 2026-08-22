package dependencies

import "testing"

func TestSelectedCoreStartupActionDelayBoundary(t *testing.T) {
	t.Parallel()
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
		InputMode:                 "STANDARD",
		StartupActions: []StartupAction{{
			Event: "GAME_START", Kind: "PRESS_CONTROL", DelayMS: 30_000,
			Player: 0, Control: 3, DurationMS: 120,
		}},
		ReportPath:            reportPath,
		SupportedContentKinds: []string{"SINGLE_FILE"},
	}
	allowlist := map[string]File{reportPath: {Path: reportPath}}
	if err := validateSelectedCore(core, allowlist); err != nil {
		t.Fatalf("30 second action rejected: %v", err)
	}
	core.StartupActions[0].DelayMS = 30_001
	if err := validateSelectedCore(core, allowlist); err == nil {
		t.Fatal("30 second plus one millisecond action accepted")
	}
}
