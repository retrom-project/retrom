package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/packs"
)

type reviewRPGMakerProjection struct {
	SelectedCoreID          string                     `json:"selectedCoreId"`
	Generation              string                     `json:"generation"`
	EvidenceGeneration      *string                    `json:"evidenceGeneration"`
	EvidenceConfidence      string                     `json:"evidenceConfidence"`
	SelfContained           bool                       `json:"selfContained"`
	SelfContainedOverride   bool                       `json:"selfContainedOverride"`
	RuntimePackRequirements []reviewRPGPackRequirement `json:"runtimePackRequirements"`
	RuntimePackSelections   []reviewRPGPackSelection   `json:"runtimePackSelections"`
}

type reviewRPGPackSelection struct {
	Slot           int64  `json:"slot"`
	DeclaredName   string `json:"declaredName"`
	InstallationID string `json:"installationId"`
}

type reviewRPGPackRequirement struct {
	Slot                   int64  `json:"slot"`
	DeclaredName           string `json:"declaredName"`
	NormalizedDeclaredName string `json:"normalizedDeclaredName"`
}

type reviewRPGAnalysisProjection struct {
	SelfContained bool `json:"selfContained"`
	Requirements  struct {
		RTP []reviewRPGPackRequirement `json:"rtpDependencies"`
	} `json:"requirements"`
}

func (server *Server) reviewRPGMaker(
	ctx context.Context,
	itemID string,
) (optionalReviewProjection, error) {
	var projection reviewRPGMakerProjection
	var evidenceGeneration sql.NullString
	var analysisJSON string
	err := server.database.QueryRowContext(ctx, `
SELECT binding.core_id,profile.generation,profile.evidence_generation,
 profile.evidence_confidence,profile.self_contained_override,
 profile.analysis_json
FROM review_drafts draft
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
JOIN runtime_target_bindings binding
  ON binding.provider_id=profile.provider_id AND binding.target_id=profile.target_id
WHERE draft.import_item_id=?
`, itemID).Scan(
		&projection.SelectedCoreID, &projection.Generation, &evidenceGeneration,
		&projection.EvidenceConfidence, &projection.SelfContainedOverride,
		&analysisJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return optionalReviewProjection{}, nil
	}
	if err != nil {
		return optionalReviewProjection{}, fmt.Errorf("review RPG Maker profile: %w", err)
	}
	projection.EvidenceGeneration = reviewOptionalString(evidenceGeneration)
	var analysis reviewRPGAnalysisProjection
	if err := json.Unmarshal([]byte(analysisJSON), &analysis); err != nil {
		return optionalReviewProjection{}, fmt.Errorf("review RPG Maker analysis: %w", err)
	}
	projection.SelfContained = analysis.SelfContained
	projection.RuntimePackRequirements = reviewRPGPackRequirements(
		projection.Generation, analysis,
	)
	selections, err := server.reviewRPGPackSelections(ctx, itemID)
	if err != nil {
		return optionalReviewProjection{}, err
	}
	projection.RuntimePackSelections = selections

	return optionalReviewProjection{value: &projection}, nil
}

func reviewRPGPackRequirements(
	generation string,
	analysis reviewRPGAnalysisProjection,
) []reviewRPGPackRequirement {
	if analysis.SelfContained {
		return []reviewRPGPackRequirement{}
	}
	if len(analysis.Requirements.RTP) != 0 {
		result := append([]reviewRPGPackRequirement(nil), analysis.Requirements.RTP...)
		for index := range result {
			result[index].NormalizedDeclaredName = packs.NormalizeDeclaredName(result[index].DeclaredName)
		}
		return result
	}
	declaredName := ""
	switch generation {
	case "RPG2000":
		declaredName = "RPG2000_RTP"
	case "RPG2003":
		declaredName = "RPG2003_RTP"
	}
	if declaredName == "" {
		return []reviewRPGPackRequirement{}
	}
	return []reviewRPGPackRequirement{{
		Slot: 0, DeclaredName: declaredName,
		NormalizedDeclaredName: packs.NormalizeDeclaredName(declaredName),
	}}
}

func (server *Server) reviewRPGPackSelections(
	ctx context.Context,
	itemID string,
) ([]reviewRPGPackSelection, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT selection.slot,selection.declared_name,selection.installation_id
FROM review_drafts draft
JOIN review_draft_runtime_pack_selections selection ON selection.review_draft_id=draft.id
WHERE draft.import_item_id=? ORDER BY selection.slot
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("review RPG Maker runtime packs: %w", err)
	}
	defer func() { cleanup.Error("close RPG Maker runtime pack rows", rows.Close()) }()
	result := []reviewRPGPackSelection{}
	for rows.Next() {
		var selection reviewRPGPackSelection
		if err := rows.Scan(&selection.Slot, &selection.DeclaredName, &selection.InstallationID); err != nil {
			return nil, fmt.Errorf("review RPG Maker runtime pack: %w", err)
		}
		result = append(result, selection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review RPG Maker runtime packs: %w", err)
	}
	return result, nil
}

func reviewOptionalString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
