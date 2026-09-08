package scummvm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotRequiresExplicitAmbiguousSelectionAndPreservesIdentity(t *testing.T) {
	games := []DetectedGame{detectedGame("one", "en"), detectedGame("two", "de")}
	result, err := parseResult(detectorJSON(t, games), Tool{UpstreamCommit: testCommit, Engines: []string{"sky"}}, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewSnapshot(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Selected(); err == nil {
		t.Fatal("ambiguous result selected implicitly")
	}
	if status, code := snapshot.Status(); status != "BLOCKED" || code != "SCUMMVM_ROOT_SELECTION_REQUIRED" {
		t.Fatalf("status %s/%s", status, code)
	}
	selected, err := snapshot.Select(result.Candidates[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ParseSnapshot(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := restored.Selected()
	if err != nil || candidate.Root != "two" || candidate.Language != "de" {
		t.Fatalf("lost selection: %+v %v", candidate, err)
	}
	if _, err := snapshot.Select(strings.Repeat("b", 64)); err == nil {
		t.Fatal("foreign selection accepted")
	}
	restored.Detection.SourceDigest = strings.Repeat("c", 64)
	encoded, err = json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSnapshot(string(encoded)); err == nil {
		t.Fatal("selection survived changed source")
	}
}

func TestSnapshotDoesNotPromoteUnsupportedCandidate(t *testing.T) {
	result, err := parseResult(detectorJSON(t, []DetectedGame{detectedGame("", "en")}), Tool{UpstreamCommit: testCommit, Engines: []string{"queen"}}, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewSnapshot(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Select(result.Candidates[0].ID); err == nil {
		t.Fatal("unavailable engine was selectable")
	}
	if status, code := snapshot.Status(); status != "BLOCKED" || code != "SCUMMVM_ENGINE_UNAVAILABLE" {
		t.Fatalf("status %s/%s", status, code)
	}
}
