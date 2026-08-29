package launch

import (
	"errors"
	"testing"
)

const validKiriKiriCompatibility = `{"adapterAbi":"kirikiri-kag-bookmark","assetsPath":"assets.zip","checkpointSlot":1999,"gameCompatibilityLine":"kirikiri2-kag-v1","jsPath":"index.js","readableSaveAbis":["kirikiri-kag-bookmark-v1"],"saveAbi":"kirikiri-kag-bookmark-v1","vlfsPath":"vlfs.js","wasmPath":"index.wasm"}`

func TestParseKiriKiriCompatibilityAcceptsExactContract(t *testing.T) {
	t.Parallel()
	value, err := parseKiriKiriCompatibility(validKiriKiriCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	if value.CheckpointSlot != 1999 || value.GameCompatibilityLine != "kirikiri2-kag-v1" ||
		value.SaveABI != "kirikiri-kag-bookmark-v1" || value.VLFSPath != "vlfs.js" {
		t.Fatalf("compatibility = %#v", value)
	}
}

func TestParseKiriKiriCompatibilityRejectsDrift(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"adapterAbi":"kirikiri-kag-bookmark","assetsPath":"assets.zip","checkpointSlot":0,"gameCompatibilityLine":"kirikiri2-kag-v1","jsPath":"index.js","readableSaveAbis":["kirikiri-kag-bookmark-v1"],"saveAbi":"kirikiri-kag-bookmark-v1","vlfsPath":"vlfs.js","wasmPath":"index.wasm"}`,
		`{"adapterAbi":"kirikiri-kag-bookmark","assetsPath":"assets.zip","checkpointSlot":1999,"gameCompatibilityLine":"kirikiri2-kag-v1","jsPath":"index.js","readableSaveAbis":["old-v1"],"saveAbi":"kirikiri-kag-bookmark-v1","vlfsPath":"vlfs.js","wasmPath":"index.wasm"}`,
		validKiriKiriCompatibility[:len(validKiriKiriCompatibility)-1] + `,"extra":true}`,
	}
	for _, input := range cases {
		if _, err := parseKiriKiriCompatibility(input); !errors.Is(err, ErrCredential) {
			t.Fatalf("parse compatibility error = %v for %s", err, input)
		}
	}
}
