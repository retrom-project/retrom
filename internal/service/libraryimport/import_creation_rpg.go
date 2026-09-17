package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/engine/rpgmaker/detector"
)

type CreationRPGManifest struct {
	FileCount   int    `json:"fileCount"`
	FilesDigest string `json:"filesDigest"`
	TotalBytes  int64  `json:"totalBytes"`
}
type creationRTPRequirement struct {
	Slot           int    `json:"slot"`
	DeclaredName   string `json:"declaredName"`
	NormalizedName string `json:"normalizedName"`
}

func (run *creationCommit) persistRPG(
	ctx context.Context,
	scope model.ImportCreationScope,
	record *creationGroup,
) error {
	profile := record.group.RPGProfile
	if profile == nil {
		return nil
	}
	target := run.plan.Target
	if target.CoreID != detector.VirtualCoreID || target.ProviderID == "" || target.TargetID == "" {
		return model.ErrInvalid
	}
	summary, err := CreationRPGManifestSummary([]byte(record.manifestJSON), len(record.group.Sources))
	if err != nil {
		return creationError("persist r p g", err)
	}
	_, requirementsDigest, err := CreationRPGRequirements(*profile)
	if err != nil {
		return creationError("persist r p g", err)
	}
	analysis, err := CreationRPGAnalysis(*profile, record.group.RPGProjectRoot, record.group.RPGRemovedFiles)
	if err != nil {
		return creationError("persist r p g", err)
	}
	dependency := sha256.Sum256([]byte(record.group.DependencySnapshot))
	change := model.CreationRPGProfile{
		DraftID:            record.draftID,
		Generation:         string(profile.ExpectedGeneration),
		EvidenceFamily:     profile.EvidenceFamily,
		EvidenceConfidence: string(profile.EvidenceConfidence),
		RequirementsDigest: requirementsDigest,
		AnalysisJSON:       string(analysis),
		FilesDigest:        summary.FilesDigest,
		ProviderID:         target.ProviderID,
		TargetID:           target.TargetID,
		DependencyDigest:   hex.EncodeToString(dependency[:]),
		EngineVersion:      creationOptional(profile.EngineVersion),
		FileCount:          summary.FileCount,
		TotalBytes:         summary.TotalBytes,
		NowMS:              run.header.NowMS,
	}
	if profile.ExpectedGeneration == detector.RPGMV || profile.ExpectedGeneration == detector.RPGMZ {
		change.EntryHTML = creationOptional("index.html")
	}
	if profile.EvidenceGeneration != nil {
		generation := string(*profile.EvidenceGeneration)
		change.EvidenceGeneration = &generation
	}
	return creationError("persist r p g", scope.Reviews.RPG(ctx, change))
}

func CreationRPGManifestSummary(contents []byte, expectedFiles int) (CreationRPGManifest, error) {
	var summary CreationRPGManifest
	if err := json.Unmarshal(contents, &summary); err != nil {
		return CreationRPGManifest{}, fmt.Errorf("decode creation RPG manifest: %w", err)
	}
	if summary.FileCount != expectedFiles || len(summary.FilesDigest) != 64 {
		return CreationRPGManifest{}, model.ErrInvalid
	}
	return summary, nil
}

func CreationRPGRequirements(profile detector.Profile) ([]byte, string, error) {
	requirements := append([]detector.Requirement{}, profile.Requirements...)
	rtp := make([]creationRTPRequirement, 0, len(profile.RTPDependencies))
	for _, dependency := range profile.RTPDependencies {
		rtp = append(
			rtp,
			creationRTPRequirement{
				Slot:           dependency.Slot,
				DeclaredName:   dependency.DeclaredName,
				NormalizedName: dependency.NormalizedName,
			},
		)
	}
	contents, err := json.Marshal(map[string]any{"requirements": requirements, "rtpDependencies": rtp})
	if err != nil {
		return nil, "", fmt.Errorf("encode creation RPG requirements: %w", err)
	}
	digest := sha256.Sum256(contents)
	return contents, hex.EncodeToString(digest[:]), nil
}

func CreationRPGAnalysis(profile detector.Profile, root string, removed []string) ([]byte, error) {
	requirements, _, err := CreationRPGRequirements(profile)
	if err != nil {
		return nil, creationError("creation r p g analysis", err)
	}
	var evidence *string
	if profile.EvidenceGeneration != nil {
		value := string(*profile.EvidenceGeneration)
		evidence = &value
	}
	contents, err := json.Marshal(
		map[string]any{
			"schemaVersion":      1,
			"selectedCoreId":     profile.SelectedCoreID,
			"expectedGeneration": profile.ExpectedGeneration,
			"evidenceGeneration": evidence,
			"evidenceFamily":     profile.EvidenceFamily,
			"evidenceConfidence": profile.EvidenceConfidence,
			"engineVersion":      creationOptional(profile.EngineVersion),
			"markerPaths":        profile.MarkerPaths,
			"selfContained":      profile.SelfContained,
			"requirements":       json.RawMessage(requirements),
			"projectRoot":        root,
			"excludedFiles":      removed,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("encode creation RPG analysis: %w", err)
	}
	return contents, nil
}
