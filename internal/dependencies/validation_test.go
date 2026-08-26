package dependencies

import (
	"strings"
	"testing"
)

func TestArtifactSetSHA256UsesCanonicalPathOrder(t *testing.T) {
	t.Parallel()
	core := SelectedCore{
		LocalPath: "data/core.data", SizeBytes: 2, SHA256: strings.Repeat("b", 64),
	}
	allowlist := []File{
		{Path: "data/core.data", SizeBytes: 99, SHA256: strings.Repeat("c", 64)},
		{Path: "data/a.js", SizeBytes: 1, SHA256: strings.Repeat("a", 64)},
	}
	const expected = "3a7ecdbf10386db178efa25e13a4e0deee49fd29c1992f254f63077c38ed55ee"
	if actual := artifactSetSHA256(allowlist, core); actual != expected {
		t.Fatalf("artifact set digest = %s, want %s", actual, expected)
	}
}

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
		AdapterABI:                "emulatorjs-state-v1",
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
	core.StartupActions[0].DelayMS = 30_000
	core.AdapterABI = "latest"
	if err := validateSelectedCore(core, allowlist); err == nil {
		t.Fatal("unlocked adapter ABI accepted")
	}
}
