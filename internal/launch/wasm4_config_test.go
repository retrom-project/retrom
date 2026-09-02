package launch

import "testing"

func TestParseWASM4CompatibilityIsClosedAndBounded(t *testing.T) {
	t.Parallel()
	valid := `{"adapterAbi":"wasm4-state-v1","cartMaxBytes":65536,"gameCompatibilityLine":"wasm4-v1","jsPath":"wasm4-retrom.mjs","readableSaveAbis":["wasm4-state-v1"],"saveAbi":"wasm4-state-v1","schemaVersion":5,"supportedContentKinds":["SINGLE_FILE"]}`
	if _, err := parseWASM4Compatibility(valid); err != nil {
		t.Fatalf("parseWASM4Compatibility(valid) error = %v", err)
	}
	for _, invalid := range []string{
		`{"adapterAbi":"wasm4-state-v1","cartMaxBytes":65537,"gameCompatibilityLine":"wasm4-v1","jsPath":"wasm4-retrom.mjs","readableSaveAbis":["wasm4-state-v1"],"saveAbi":"wasm4-state-v1","schemaVersion":5,"supportedContentKinds":["SINGLE_FILE"]}`,
		`{"adapterAbi":"wasm4-state-v1","cartMaxBytes":65536,"gameCompatibilityLine":"wasm4-v1","jsPath":"wasm4-retrom.mjs","readableSaveAbis":["wasm4-state-v1"],"saveAbi":"wasm4-state-v1","schemaVersion":5,"supportedContentKinds":["SINGLE_FILE"],"extra":true}`,
	} {
		if _, err := parseWASM4Compatibility(invalid); err == nil {
			t.Fatalf("parseWASM4Compatibility(%s) succeeded", invalid)
		}
	}
}
