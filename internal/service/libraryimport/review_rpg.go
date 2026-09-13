package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/engine/rpgmaker/detector"
)

type RPGReviewAnalysis struct {
	SelfContained bool `json:"selfContained"`
	Requirements  struct {
		RTP []detector.RTPDependency `json:"rtpDependencies"`
	} `json:"requirements"`
}
type RPGReviewProfile struct {
	SelectedCoreID, Generation, EvidenceConfidence, DependencySHA256, AnalysisJSON string
	EvidenceGeneration                                                             *string
	SelfContainedOverride                                                          bool
}
type RPGReviewDependencies struct{ Status, Code, SnapshotJSON, Digest string }

func ResolveRPGReviewDependencies(profile RPGReviewProfile) (RPGReviewDependencies, error) {
	var analysis RPGReviewAnalysis
	if err := json.Unmarshal([]byte(profile.AnalysisJSON), &analysis); err != nil {
		return RPGReviewDependencies{}, fmt.Errorf("decode RPG review analysis: %w", err)
	}
	return ResolveRPGResourcePolicy(profile.Generation, profile.SelfContainedOverride, analysis), nil
}

func ResolveRPGResourcePolicy(generation string, override bool, analysis RPGReviewAnalysis) RPGReviewDependencies {
	requirements := detector.ExternalRTPRequirements(detector.Generation(generation),
		analysis.SelfContained,
		analysis.Requirements.RTP)
	state := RPGReviewDependencies{Status: "READY", Code: "READY"}
	if len(requirements) > 0 && !override {
		state.Status, state.Code = "BLOCKED", "RPG_EXTERNAL_RTP_REQUIRED"
	}
	snapshot, _ := json.Marshal(map[string]any{
		"schemaVersion":         2,
		"policy":                "PROJECT_RESOURCES_ONLY",
		"externalRTP":           requirements,
		"selfContainedOverride": override,
	})
	state.SnapshotJSON = string(snapshot)
	digest := sha256.Sum256(snapshot)
	state.Digest = hex.EncodeToString(digest[:])
	return state
}
