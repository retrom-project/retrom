package refactor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fingerprintedInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type characterizationProvenance struct {
	SchemaVersion         int                            `json:"schemaVersion"`
	BaselineCommit        string                         `json:"baselineCommit"`
	BaselineTree          string                         `json:"baselineTree"`
	BaselineArchiveSHA256 string                         `json:"baselineArchiveSha256"`
	Snapshot              fingerprintedInput             `json:"snapshot"`
	FixedNowMS            int64                          `json:"fixedNowMs"`
	PublicInputs          []fingerprintedInput           `json:"publicInputs"`
	CaptureHarness        []fingerprintedInput           `json:"captureHarness"`
	Normalizations        []struct{ Field, Rule string } `json:"normalizations"`
	Coverage              []string                       `json:"coverage"`
}

func TestCharacterizationProvenancePinsFrozenInputs(t *testing.T) {
	t.Parallel()
	root := fixtureRepositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "quality/architecture/characterizations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance characterizationProvenance
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&provenance); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		t.Fatal("trailing provenance document")
	}
	if provenance.SchemaVersion != 1 || provenance.BaselineCommit != "82834bade1648da3067ebda1b1fc89c18577fd6a" ||
		provenance.FixedNowMS != 1786000000000 {
		t.Fatal("baseline provenance or fixed fixture time changed")
	}
	if len(provenance.Normalizations) != 1 || provenance.Normalizations[0].Field != "Blob.RelativePath" {
		t.Fatal("normalization may not remove state, authority, ownership, version or other data")
	}
	if len(provenance.PublicInputs) != 2 || len(provenance.CaptureHarness) != 4 || len(provenance.Coverage) != 5 {
		t.Fatal("baseline capture input coverage changed")
	}
	inputs := append([]fingerprintedInput{provenance.Snapshot}, provenance.PublicInputs...)
	for _, input := range inputs {
		verifyCharacterizationInput(t, root, input)
	}

	if provenance.Snapshot.Path != "internal/testkit/refactor/testdata/baseline-82834ba.json" {
		t.Fatal("baseline snapshot source changed")
	}
}

func fixtureRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture source unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func verifyCharacterizationInput(t *testing.T, root string, input fingerprintedInput) {
	t.Helper()
	if path.Clean(input.Path) != input.Path || strings.HasPrefix(input.Path, "/") || strings.Contains(input.Path, "..") {
		t.Fatal("unsafe characterization input path")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(input.Path)))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != input.SHA256 {
		t.Fatalf("frozen characterization input changed: %s", input.Path)
	}
}
