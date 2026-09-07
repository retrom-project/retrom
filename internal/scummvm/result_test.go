package scummvm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const testCommit = "fed42f2068dcafc6aafa1c28c77e4c88def74b66"

func detectorJSON(t *testing.T, candidates []DetectedGame) []byte {
	t.Helper()
	value, err := json.Marshal(detectorResult{SchemaVersion: 1, UpstreamCommit: testCommit, Candidates: candidates})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func detectedGame(root, language string) DetectedGame {
	return DetectedGame{
		Root: root, EngineID: "sky", GameID: "sky", Description: "Beneath a Steel Sky",
		Language: language, Platform: "pc", Extra: "v0.0348 Floppy", PreferredTarget: "sky",
		GUIOptions: "sndNoSpeech", CanBeAdded: true, Config: map[string]string{},
	}
}

func TestDetectionPreservesAmbiguityAndLaunchHints(t *testing.T) {
	candidates := []DetectedGame{detectedGame("one", "en"), detectedGame("one", "de"), detectedGame("two", "en")}
	candidates[1].Config["filename"] = "story.z5"
	result, err := parseResult(detectorJSON(t, candidates), Tool{UpstreamCommit: testCommit, Engines: []string{"sky"}},
		strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 3 || result.AutomaticSelection != "" || len(result.Roots) != 2 {
		t.Fatalf("ambiguity was lost: %+v", result)
	}
	if result.Candidates[1].Config["filename"] != "story.z5" || result.Candidates[0].Extra != "v0.0348 Floppy" {
		t.Fatal("upstream launch hints were lost")
	}
	if result.Candidates[0].ID == result.Candidates[1].ID {
		t.Fatal("language variants must have different candidate identities")
	}
}

func TestAutomaticSelectionRequiresOneRunnableCandidate(t *testing.T) {
	tool := Tool{UpstreamCommit: testCommit, Engines: []string{"sky"}}
	candidate := detectedGame("", "en")
	result, err := parseResult(detectorJSON(t, []DetectedGame{candidate}), tool, strings.Repeat("a", 64))
	if err != nil || result.AutomaticSelection == "" {
		t.Fatalf("unique runnable candidate was not selected: %v", err)
	}
	tool.Engines = []string{"queen"}
	result, err = parseResult(detectorJSON(t, []DetectedGame{candidate}), tool, strings.Repeat("a", 64))
	if err != nil || result.AutomaticSelection != "" || result.Candidates[0].Blocker != "ENGINE_UNAVAILABLE" {
		t.Fatalf("missing engine was hidden or selected: %+v %v", result, err)
	}
	candidate.HasUnknownFiles = true
	tool.Engines = []string{"sky"}
	result, err = parseResult(detectorJSON(t, []DetectedGame{candidate}), tool, strings.Repeat("a", 64))
	if err != nil || result.AutomaticSelection != "" || result.Candidates[0].Blocker != "UNKNOWN_VARIANT" {
		t.Fatalf("unknown variant was selected: %+v %v", result, err)
	}
}

func TestDetectionRejectsBadPathsAndMismatchedBuild(t *testing.T) {
	tool := Tool{UpstreamCommit: testCommit, Engines: []string{"sky"}}
	for _, root := range []string{"../outside", "/absolute", "a/../b", "a\\b", "a\nkey=value"} {
		_, err := parseResult(detectorJSON(t, []DetectedGame{detectedGame(root, "en")}), tool, strings.Repeat("a", 64))
		if !errors.Is(err, ErrResultInvalid) {
			t.Fatalf("unsafe root %q accepted: %v", root, err)
		}
	}
	tool.UpstreamCommit = strings.Repeat("b", 40)
	_, err := parseResult(detectorJSON(t, []DetectedGame{detectedGame("", "en")}), tool, strings.Repeat("a", 64))
	if !errors.Is(err, ErrResultInvalid) {
		t.Fatalf("mismatched detector build accepted: %v", err)
	}
}

func TestCandidateIdentityChangesWithSourceSnapshot(t *testing.T) {
	tool := Tool{UpstreamCommit: testCommit, Engines: []string{"sky"}}
	input := detectorJSON(t, []DetectedGame{detectedGame("", "en")})
	first, err := parseResult(input, tool, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseResult(input, tool, strings.Repeat("b", 64))
	if err != nil || first.Candidates[0].ID == second.Candidates[0].ID {
		t.Fatalf("stale source selection identity was reused: %v", err)
	}
}
