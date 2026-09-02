package dependencies

import (
	"encoding/json"
	"testing"
)

func TestWASM4CandidateArtifactContractIsClosed(t *testing.T) {
	t.Parallel()
	artifact := RPGMakerArtifact{
		CoreID: "wasm4", RuntimeFamily: "WASM4", Generation: "WASM4",
		RouteKey: "WASM4_WEB", RuntimeAdapterKind: "WASM4_WEB",
		RuntimeVersion: "v0.11.2", AdapterID: "wasm4-web", AdapterABI: "wasm4-state-v1",
		EntryPath: "wasm4-retrom.mjs", RequiresThreads: false,
		SavePayloadKind: "RUNTIME_STATE", SaveMaxBytes: 132144,
		SelectedForNewBindings: true, AvailableForLaunch: true,
		Compatibility: json.RawMessage(`{
			"adapterAbi":"wasm4-state-v1","cartMaxBytes":65536,
			"gameCompatibilityLine":"wasm4-v1","jsPath":"wasm4-retrom.mjs",
			"readableSaveAbis":["wasm4-state-v1"],"saveAbi":"wasm4-state-v1",
			"schemaVersion":5,"supportedContentKinds":["SINGLE_FILE"]
		}`),
	}
	if !validWASM4Identity(artifact) || !validWASM4Compatibility(artifact) {
		t.Fatalf("WASM-4 candidate contract rejected: %#v", artifact)
	}
	artifact.SaveMaxBytes++
	if validWASM4Identity(artifact) {
		t.Fatal("WASM-4 identity accepted an expanded checkpoint bound")
	}
}
