package gamevariant

import (
	"encoding/json"
	"testing"

	"retrom/internal/content/requirements"
)

func TestArcadeRevalidationRetainsInputRejectionAndLockedDATFacts(t *testing.T) {
	_, repository, _ := newValidationTestWorker(t)
	facts := repository.facts
	source := &facts.Content.Source
	dat := "dat"
	source.ProviderID, source.TargetID, source.CoreID, source.PlatformID = "emulatorjs", "fbneo", "fbneo", "arcade"
	source.ValidationLogicalName = "cart.zip"
	source.DATVersionID, source.ActiveDATVersionID = &dat, &dat
	source.DependencySnapshot = `{"schemaVersion":1,"kind":"ARCADE","machine":"cart","datVersionId":"dat","closure":[],"dependencies":[],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`
	source.ContentPolicy.InputMaxFileBytes = map[string]int64{"game": 10}
	facts.Content.GameFiles = []File{{Role: "CONTENT", LogicalName: "cart.zip", SizeBytes: 11}}
	facts.Classification = "NORMAL"
	inputs, err := Inputs(facts.Content, "variant")
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := EvaluateValidation(inputs, facts)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Machine   string `json:"machine"`
		DAT       string `json:"datVersionId"`
		Rejection struct {
			Code string `json:"code"`
		} `json:"contentRejection"`
	}
	if err := json.Unmarshal([]byte(outcome.DependencyJSON), &snapshot); err != nil ||
		outcome.Code != "CONTENT_FILE_BYTES_EXCEEDED" || snapshot.Rejection.Code != outcome.Code ||
		snapshot.Machine != "cart" || snapshot.DAT != dat {
		t.Fatalf("lost content or DAT evidence: %+v %+v %v", outcome, snapshot, err)
	}
}

func TestTargetPolicyChangeInvalidatesValidationInput(t *testing.T) {
	_, repository, _ := newValidationTestWorker(t)
	before, err := Inputs(repository.facts.Content, "variant")
	if err != nil {
		t.Fatal(err)
	}
	repository.facts.Content.Source.ContentPolicy.InputMaxFileBytes = map[string]int64{"game": 100}
	limited, err := Inputs(repository.facts.Content, "variant")
	if err != nil {
		t.Fatal(err)
	}
	if before.ValidationInputDigest == limited.ValidationInputDigest {
		t.Fatal("changed input limit retained old validation")
	}
	repository.facts.Content.Source.ContentPolicy.Requirements = &requirements.Policy{Kind: requirements.Decrypted3DS}
	encrypted, err := Inputs(repository.facts.Content, "variant")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted.ValidationInputDigest == limited.ValidationInputDigest {
		t.Fatal("new content requirement retained old validation")
	}
}
