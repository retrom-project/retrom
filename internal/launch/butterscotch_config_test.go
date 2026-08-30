package launch

import "testing"

const validButterscotchCompatibility = `{"adapterAbi":"butterscotch-checkpoint-v2","gameCompatibilityLine":"butterscotch-gamemaker-v1","jsPath":"butterscotch.mjs","readableSaveAbis":["butterscotch-checkpoint-v2"],"saveAbi":"butterscotch-checkpoint-v2","wasmPath":"butterscotch.wasm","workerPath":"butterscotch-worker.mjs"}`

func TestParseButterscotchCompatibility(t *testing.T) {
	t.Parallel()
	value, err := parseButterscotchCompatibility(validButterscotchCompatibility)
	if err != nil || value.WorkerPath != "butterscotch-worker.mjs" ||
		value.GameCompatibilityLine != "butterscotch-gamemaker-v1" {
		t.Fatalf("value=%#v error=%v", value, err)
	}
	for _, invalid := range []string{
		`{}`,
		`{"adapterAbi":"butterscotch-checkpoint-v1","gameCompatibilityLine":"butterscotch-gamemaker-v1","jsPath":"butterscotch.mjs","readableSaveAbis":["butterscotch-checkpoint-v1"],"saveAbi":"butterscotch-checkpoint-v1","wasmPath":"butterscotch.wasm","workerPath":"butterscotch-worker.mjs"}`,
		`{"adapterAbi":"butterscotch-checkpoint-v2","gameCompatibilityLine":"butterscotch-gamemaker-v1","jsPath":"butterscotch.mjs","readableSaveAbis":["old-v1"],"saveAbi":"butterscotch-checkpoint-v2","wasmPath":"butterscotch.wasm","workerPath":"butterscotch-worker.mjs"}`,
		validButterscotchCompatibility[:len(validButterscotchCompatibility)-1] + `,"extra":true}`,
	} {
		if _, err := parseButterscotchCompatibility(invalid); err == nil {
			t.Fatalf("accepted invalid compatibility %s", invalid)
		}
	}
}
