package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/core/rpgmaker/detector"
)

func readReviewValidation(ctx context.Context, scope ReviewReadScope, head ReviewHead, result *ReviewDetail) error {
	var dependency json.RawMessage
	var err error
	if head.DependencyJSON != nil {
		dependency, err = reviewDocument(*head.DependencyJSON)
		if err != nil {
			return err
		}
	}
	result.Readiness = &ReviewReadinessView{
		Status:            optionalTextValue(head.ValidationStatus),
		CompatibilityCode: optionalTextValue(head.CompatibilityCode), DependencySnapshot: dependency,
	}
	screenshot, found, err := scope.Media.RuntimeScreenshot(ctx, head.ItemID)
	if err != nil {
		return fmt.Errorf("read review runtime screenshot: %w", err)
	}
	if found {
		result.RuntimeScreenshot = &screenshot
	}
	ready := optionalTextValue(head.ValidationStatus) == "READY"
	result.CanApprove = head.Policy.Supports(head.ContentKind) && ReviewApproval(head.ContentKind, ready, found)
	profile, found, err := scope.Profiles.Profile(ctx, head.DraftID)
	if err != nil {
		return fmt.Errorf("read review RPG profile: %w", err)
	}
	if found {
		result.RPGMaker, err = ProjectReviewRPGMaker(profile)
	}
	return err
}

func ProjectReviewRPGMaker(profile RPGReviewProfile) (*ReviewRPGMaker, error) {
	var analysis RPGReviewAnalysis
	if err := json.Unmarshal([]byte(profile.AnalysisJSON), &analysis); err != nil {
		return nil, fmt.Errorf("decode review RPG profile: %w", err)
	}
	dependencies := detector.ExternalRTPRequirements(detector.Generation(profile.Generation),
		analysis.SelfContained,
		analysis.Requirements.RTP)
	requirements := make([]ReviewRTPDeclaration, 0, len(dependencies))
	for _, entry := range dependencies {
		requirements = append(requirements, ReviewRTPDeclaration{Slot: int64(entry.Slot), DeclaredName: entry.DeclaredName})
	}
	return &ReviewRPGMaker{
		SelectedCoreID:     profile.SelectedCoreID,
		Generation:         profile.Generation,
		EvidenceGeneration: profile.EvidenceGeneration,

		EvidenceConfidence:    profile.EvidenceConfidence,
		SelfContained:         analysis.SelfContained,
		SelfContainedOverride: profile.SelfContainedOverride,

		ExternalRTPRequirements: requirements,
	}, nil
}

func ReviewApproval(contentKind string, selectedReady, hasCurrentScreenshot bool) bool {
	return selectedReady || contentKind != "SCUMMVM_PROJECT" && hasCurrentScreenshot
}
