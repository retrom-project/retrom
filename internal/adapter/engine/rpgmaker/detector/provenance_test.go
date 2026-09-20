package detector

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestOldDetectorGoldenAndInputsKeepCapturedHashes(t *testing.T) {
	t.Parallel()
	provenance := fixtureValue[struct {
		Golden string `json:"old_golden_sha256"`
		Inputs string `json:"fixture_inputs_sha256"`
		Bounds string `json:"bounded_inputs_sha256"`
		Source string `json:"input_capture_source_sha256"`
		Cases  int    `json:"capture_cases"`
	}](t, "provenance.json")
	if provenance.Cases != 122 ||
		provenance.Golden != "1ce3b90ccead224cca1a9f28d4d4a3d122a78aca8f3bb1cbd06de27aa5edb439" {
		t.Fatal("original detector observation identity changed")
	}
	for name, expected := range map[string]string{
		"old-golden.json": provenance.Golden, "fixture-inputs.json": provenance.Inputs,
		"bounded-inputs.json": provenance.Bounds, "old-capture.go.txt": provenance.Source,
	} {
		contents, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		if hex.EncodeToString(digest[:]) != expected {
			t.Fatalf("captured %s SHA changed", name)
		}
	}
}
