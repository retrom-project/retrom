package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/rpgmaker/detector"
)

type reviewRPGMakerProjection struct {
	SelectedCoreID          string                 `json:"selectedCoreId"`
	Generation              string                 `json:"generation"`
	EvidenceGeneration      *string                `json:"evidenceGeneration"`
	EvidenceConfidence      string                 `json:"evidenceConfidence"`
	SelfContained           bool                   `json:"selfContained"`
	SelfContainedOverride   bool                   `json:"selfContainedOverride"`
	ExternalRTPRequirements []reviewRTPDeclaration `json:"externalRTPRequirements"`
}

type reviewRTPDeclaration struct {
	Slot         int64  `json:"slot"`
	DeclaredName string `json:"declaredName"`
}

type reviewRPGAnalysisProjection struct {
	SelfContained bool `json:"selfContained"`
	Requirements  struct {
		RTP []reviewRTPDeclaration `json:"rtpDependencies"`
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
	projection.ExternalRTPRequirements = reviewExternalRTPRequirements(
		projection.Generation, analysis,
	)

	return optionalReviewProjection{value: &projection}, nil
}

func reviewExternalRTPRequirements(generation string, analysis reviewRPGAnalysisProjection) []reviewRTPDeclaration {
	declared := make([]detector.RTPDependency, 0, len(analysis.Requirements.RTP))
	for _, entry := range analysis.Requirements.RTP {
		declared = append(declared, detector.RTPDependency{Slot: int(entry.Slot), DeclaredName: entry.DeclaredName})
	}
	dependencies := detector.ExternalRTPRequirements(detector.Generation(generation), analysis.SelfContained, declared)
	result := make([]reviewRTPDeclaration, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, reviewRTPDeclaration{Slot: int64(dependency.Slot), DeclaredName: dependency.DeclaredName})
	}
	return result
}

func reviewOptionalString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
