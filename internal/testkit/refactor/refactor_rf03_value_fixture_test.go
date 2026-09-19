package refactor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jobsmodel "retrom/internal/model/jobs"
	metadatamodel "retrom/internal/model/metadata"
)

type rf03ValueInput struct {
	FixedNow    string                      `json:"fixedNow"`
	HTTPRawBody string                      `json:"httpRawBody"`
	Hashes      metadatamodel.ContentHashes `json:"hashes"`
	BlobBody    string                      `json:"blobBody"`
	Jobs        []jobsmodel.Snapshot        `json:"jobs"`
	Saves       []rf03SaveInput             `json:"saves"`
}

type rf03SaveInput struct {
	Label            string  `json:"label"`
	ResourceKind     string  `json:"resourceKind"`
	SaveStateID      string  `json:"saveStateId"`
	PreviewID        string  `json:"previewId"`
	CheckpointFormat string  `json:"checkpointFormat"`
	ScreenshotURL    *string `json:"screenshotUrl"`
	CreatedAtMS      int64   `json:"createdAtMs"`
	Name             string  `json:"name"`
	DiscIndex        *int    `json:"discIndex"`
	Version          int64   `json:"version"`
	ActiveDurationMS int64   `json:"activeDurationMs"`
}

type (
	rf03CapturedFile      struct{ Path, SHA256 string }
	rf03CaptureProvenance struct {
		BaselineCommit, GoVersion, InputFile, InputSHA256    string
		CaptureProgram, CaptureProgramSHA256, CaptureCommand string
		Sources                                              []rf03CapturedFile
	}
)

type rf03CapturedMetadata struct {
	CandidateJSON, NullCandidateJSON, EmptyCandidateJSON      string
	RawResponseBase64, RequestBody, RequestDigest, HashDigest string
	RequestMethod, RequestURL, Outcome                        string
}
type (
	rf03CapturedSave   struct{ Label, JSON string }
	rf03CapturedValues struct {
		SchemaVersion int
		Provenance    rf03CaptureProvenance
		Metadata      rf03CapturedMetadata
		JobJSON       []string
		Saves         []rf03CapturedSave
		BlobFactsJSON string
	}
)

func rf03ValueCompatibilityFixture(t *testing.T) (rf03ValueInput, rf03CapturedValues) {
	t.Helper()
	var golden rf03CapturedValues
	rf03ReadJSON(t, filepath.Join("testdata", "rf03-values-82834ba.json"), &golden)
	rf03AssertCaptureProvenance(t, golden)
	provenance := golden.Provenance
	inputBytes := rf03VerifiedFixture(t, provenance.InputFile, provenance.InputSHA256)
	rf03VerifiedFixture(t, provenance.CaptureProgram, provenance.CaptureProgramSHA256)
	var input rf03ValueInput
	if err := json.Unmarshal(inputBytes, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Jobs) != 2 || len(golden.JobJSON) != 2 || len(input.Saves) != 3 || len(golden.Saves) != 3 {
		t.Fatal("frozen job/save cases are missing")
	}
	return input, golden
}

func rf03VerifiedFixture(t *testing.T, name, expectedSHA256 string) []byte {
	t.Helper()
	value, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(value)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		t.Fatalf("captured input/source bytes changed: %s", name)
	}
	return value
}

func rf03ReadJSON(t *testing.T, name string, target any) {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, target); err != nil {
		t.Fatal(err)
	}
}

func rf03AssertJSONBytes(t *testing.T, value any, expected string) {
	t.Helper()
	actual, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != expected {
		t.Fatalf("value bytes changed:\nactual: %s\nfrozen: %s", actual, expected)
	}
}

func rf03AssertCaptureProvenance(t *testing.T, golden rf03CapturedValues) {
	t.Helper()
	provenance := golden.Provenance
	if golden.SchemaVersion != 1 || provenance.BaselineCommit != "82834bade1648da3067ebda1b1fc89c18577fd6a" ||
		provenance.GoVersion != "go1.26.5" || len(provenance.Sources) != 10 {
		t.Fatalf("RF03 frozen capture provenance changed: %+v", provenance)
	}
	if provenance.InputFile != "rf03-values-input.json" || provenance.CaptureProgram != "rf03-value-capture.go.txt" {
		t.Fatalf("unexpected capture inputs: %+v", provenance)
	}
	expectedCommand := "GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly go run ./internal/rf03valuecapture ../../../internal/testkit/refactor/testdata/rf03-values-input.json"
	if provenance.CaptureCommand != expectedCommand {
		t.Fatalf("capture command changed: %s", provenance.CaptureCommand)
	}
	rf03AssertCapturedSources(t, provenance.Sources)
}

func rf03AssertCapturedSources(t *testing.T, sources []rf03CapturedFile) {
	t.Helper()
	for _, source := range sources {
		hash, err := hex.DecodeString(source.SHA256)
		if err != nil || len(hash) != sha256.Size || source.Path == "" || strings.HasPrefix(source.Path, "/") {
			t.Fatalf("invalid captured source fact: %+v", source)
		}
	}
}
