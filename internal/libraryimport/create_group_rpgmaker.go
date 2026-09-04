package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/rpgmaker/detector"
)

type rpgManifestSummary struct {
	FileCount   int    `json:"fileCount"`
	FilesDigest string `json:"filesDigest"`
	TotalBytes  int64  `json:"totalBytes"`
}

type rpgRTPRequirementJSON struct {
	Slot           int    `json:"slot"`
	DeclaredName   string `json:"declaredName"`
	NormalizedName string `json:"normalizedName"`
}

func (run *creationRun) persistRPGMakerReviewProfile(record *groupRecord) error {
	profile := record.group.rpgProfile
	if profile == nil {
		return nil
	}
	target := run.plan.target
	if target.coreID != detector.VirtualCoreID || target.providerID == "" || target.targetID == "" {
		return ErrInvalid
	}
	summary, err := loadRPGManifestSummary(record.manifestJSON, len(record.group.sources))
	if err != nil {
		return ErrInvalid
	}
	_, requirementsSHA := rpgRequirements(*profile)
	analysisJSON, err := rpgAnalysis(*profile, record.group.rpgProjectRoot, record.group.rpgRemovedFiles)
	if err != nil {
		return err
	}
	dependencyDigest := sha256.Sum256([]byte(record.group.dependencySnapshot))
	entryHTML := rpgEntryHTML(profile.ExpectedGeneration)
	evidenceGeneration := rpgEvidenceGeneration(profile.EvidenceGeneration)
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO rpgmaker_review_profiles(
  review_draft_id,generation,evidence_family,evidence_generation,
  evidence_confidence,engine_version,entry_html_path,file_count,total_bytes,project_fingerprint,
  requirements_sha256,analysis_json,self_contained_override,provider_id,target_id,
  dependency_snapshot_sha256,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`, record.draftID, profile.ExpectedGeneration, profile.EvidenceFamily,
		evidenceGeneration, profile.EvidenceConfidence, nullableString(profile.EngineVersion), entryHTML,
		summary.FileCount, summary.TotalBytes, summary.FilesDigest, requirementsSHA, string(analysisJSON),
		0, target.providerID, target.targetID, hex.EncodeToString(dependencyDigest[:]), run.now, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/rpgmaker profile: %w", err)
	}
	return nil
}

func loadRPGManifestSummary(contents []byte, expectedFiles int) (rpgManifestSummary, error) {
	var summary rpgManifestSummary
	if json.Unmarshal(contents, &summary) != nil || summary.FileCount != expectedFiles || len(summary.FilesDigest) != 64 {
		return rpgManifestSummary{}, ErrInvalid
	}
	return summary, nil
}

func rpgEntryHTML(generation detector.Generation) any {
	if generation == detector.RPGMV || generation == detector.RPGMZ {
		return "index.html"
	}
	return nil
}

func rpgEvidenceGeneration(generation *detector.Generation) any {
	if generation == nil {
		return nil
	}
	return string(*generation)
}

func rpgRequirements(profile detector.Profile) ([]byte, string) {
	requirements := append([]detector.Requirement(nil), profile.Requirements...)
	if requirements == nil {
		requirements = []detector.Requirement{}
	}
	rtp := make([]rpgRTPRequirementJSON, 0, len(profile.RTPDependencies))
	for _, dependency := range profile.RTPDependencies {
		rtp = append(rtp, rpgRTPRequirementJSON{
			Slot: dependency.Slot, DeclaredName: dependency.DeclaredName,
			NormalizedName: dependency.NormalizedName,
		})
	}
	contents, _ := json.Marshal(map[string]any{
		"requirements":    requirements,
		"rtpDependencies": rtp,
	})
	digest := sha256.Sum256(contents)
	return contents, hex.EncodeToString(digest[:])
}

func rpgAnalysis(profile detector.Profile, root string, removed []string) ([]byte, error) {
	requirementsJSON, _ := rpgRequirements(profile)
	var requirements any
	if json.Unmarshal(requirementsJSON, &requirements) != nil {
		return nil, ErrInvalid
	}
	evidenceGeneration := any(nil)
	if profile.EvidenceGeneration != nil {
		evidenceGeneration = *profile.EvidenceGeneration
	}
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "selectedCoreId": profile.SelectedCoreID,
		"expectedGeneration": profile.ExpectedGeneration, "evidenceGeneration": evidenceGeneration,
		"evidenceFamily": profile.EvidenceFamily, "evidenceConfidence": profile.EvidenceConfidence,
		"engineVersion": nullableString(profile.EngineVersion), "markerPaths": profile.MarkerPaths,
		"selfContained": profile.SelfContained,
		"requirements":  requirements, "projectRoot": root, "excludedFiles": removed,
	})
	if err != nil {
		return nil, fmt.Errorf("libraryimport/rpgmaker analysis: %w", err)
	}
	return contents, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
