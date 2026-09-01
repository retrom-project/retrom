package dependencies

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRPGMakerPFBCandidateRequiresMatchingIdentityAndFormalManifest(t *testing.T) {
	runtimeRoot := t.TempDir()
	identifier := "feature-a1b2c3d4e5f6"
	formalSHA := strings.Repeat("a", 64)
	marker := map[string]any{
		"schemaVersion": 1, "kind": "RETROM_PFB_CANDIDATE_V1", "pfbId": identifier,
		"formalManifestSha256": formalSHA, "runtime": map[string]any{}, "cores": []any{},
		"runtimeFiles": []any{}, "artifacts": []any{}, "filesSha256": strings.Repeat("b", 64),
		"overlaySha256": "",
	}
	contents, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	marker["overlaySha256"], err = calculateRPGMakerPFBOverlaySHA256(contents)
	if err != nil {
		t.Fatal(err)
	}
	contents, err = json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, ".retrom-pfb-candidate.json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RETROM_PFB_ID", identifier)
	candidate, enabled, err := loadRPGMakerPFBCandidate(runtimeRoot, formalSHA)
	if err != nil || !enabled || candidate.PFBID != identifier {
		t.Fatalf("candidate = %#v, enabled = %t, error = %v", candidate, enabled, err)
	}
	if _, _, err := loadRPGMakerPFBCandidate(runtimeRoot, strings.Repeat("d", 64)); err == nil {
		t.Fatal("candidate with a mismatched formal manifest was accepted")
	}
	marker["filesSha256"] = strings.Repeat("d", 64)
	contents, err = json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, ".retrom-pfb-candidate.json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadRPGMakerPFBCandidate(runtimeRoot, formalSHA); err == nil {
		t.Fatal("candidate with bytes outside its overlay digest was accepted")
	}
}
