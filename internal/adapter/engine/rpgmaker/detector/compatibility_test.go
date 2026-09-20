package detector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
)

type capturedErrorPart struct{ Type, Text string }

type capturedObservation struct {
	Name, Operation, Core                 string
	ProfileJSON                           string
	ErrorChain                            []capturedErrorPart
	DetectionError                        *policy.Error
	IsOpen, IsRead, IsClose               bool
	FilesCalls, MaxActive, ActiveAtReturn int
	IO                                    []fileStats
	BytesReturned                         int
	BytesSHA256                           string
}

func observe(
	t *testing.T, want capturedObservation, profile policy.Profile, err error, index *traceIndex, contents []byte,
) capturedObservation {
	t.Helper()
	encoded, marshalErr := json.Marshal(profile)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	result := capturedObservation{
		Name: want.Name, Operation: want.Operation, Core: want.Core, ProfileJSON: string(encoded),
		IsOpen: errors.Is(err, errCapturedOpen), IsRead: errors.Is(err, errCapturedRead),
		IsClose: errors.Is(err, errCapturedClose),
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		result.ErrorChain = append(result.ErrorChain, capturedErrorPart{Type: fmt.Sprintf("%T", cause), Text: cause.Error()})
	}
	if err != nil {
		errors.As(err, &result.DetectionError)
	}
	if index != nil {
		result.FilesCalls, result.MaxActive, result.ActiveAtReturn = index.filesCalls, index.maxActive, index.active
		for _, stats := range index.stats {
			result.IO = append(result.IO, *stats)
		}
		sort.Slice(result.IO, func(i, j int) bool { return result.IO[i].Path < result.IO[j].Path })
	}
	if want.Operation == "catalog.read" {
		result.BytesReturned = len(contents)
		hash := sha256.Sum256(contents)
		result.BytesSHA256 = hex.EncodeToString(hash[:])
	}
	return result
}

func assertObservation(t *testing.T, got, want capturedObservation) {
	t.Helper()
	// Error's private detail/cause are checked by ErrorChain and errors.Is above.
	gotError, err := json.Marshal(got.DetectionError)
	if err != nil {
		t.Fatal(err)
	}
	wantError, err := json.Marshal(want.DetectionError)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotError) != string(wantError) {
		t.Fatalf("typed detection error = %s, want %s", gotError, wantError)
	}
	got.DetectionError, want.DetectionError = nil, nil
	if !reflect.DeepEqual(got, want) {
		gotJSON, gotErr := json.MarshalIndent(got, "", "  ")
		wantJSON, wantErr := json.MarshalIndent(want, "", "  ")
		if gotErr != nil || wantErr != nil {
			t.Fatalf("marshal observations: %v / %v", gotErr, wantErr)
		}
		t.Fatalf("actual observation:\n%s\nold Go observation:\n%s", gotJSON, wantJSON)
	}
}

func TestDetectorReplaysAll122OldGoObservations(t *testing.T) {
	old := fixtureValue[[]capturedObservation](t, "old-golden.json")
	if len(old) != 122 {
		t.Fatalf("old observation count = %d, want 122", len(old))
	}
	inputs := make(map[string]capturedInput)
	for _, input := range fixtureValue[[]capturedInput](t, "fixture-inputs.json") {
		inputs[input.Name] = input
	}
	bounds := make(map[string]capturedBound)
	for _, bound := range fixtureValue[[]capturedBound](t, "bounded-inputs.json") {
		bounds[bound.Name] = bound
	}
	for _, want := range old {
		t.Run(want.Name, func(t *testing.T) {
			if bound, exists := bounds[want.Name]; exists {
				assertObservation(t, replayBound(t, want, bound), want)
				return
			}
			if input, exists := inputs[want.Name]; exists {
				index := newTraceIndex(capturedSource(t, input))
				index.sizes, index.target, index.mode = input.Sizes, input.Target, input.Mode
				profile, err := detect(input.Core, index)
				assertObservation(t, observe(t, want, profile, err, index, nil), want)
				return
			}
			if want.Name != "index/nil" && want.Name != "index/unsupported-before-nil" {
				t.Fatalf("no original input for %q", want.Name)
			}
			profile, err := detect(want.Core, nil)
			assertObservation(t, observe(t, want, profile, err, nil, nil), want)
		})
	}
}
