package runtimebundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOrdinaryReviewEnvelopeDoesNotRequireProofProtocol(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join("..", "..", "api", "runtime-provider", "v1", "fixtures", "valid", "single-minimal.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(contents, &envelope); err != nil {
		t.Fatal(err)
	}
	delete(envelope, "validation")
	session, ok := envelope["session"].(map[string]any)
	if !ok {
		t.Fatal("fixture session missing")
	}
	session["purpose"] = "REVIEW_PREVIEW"
	runtime, ok := envelope["runtime"].(map[string]any)
	if !ok {
		t.Fatal("fixture runtime missing")
	}
	capabilities, ok := runtime["capabilities"].(map[string]any)
	if !ok {
		t.Fatal("fixture capabilities missing")
	}
	delete(capabilities, "validationProbes")
	contents, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseLaunchEnvelope(contents); err != nil {
		t.Fatalf("ordinary review still requires production proof plumbing: %v", err)
	}
}
