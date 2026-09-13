package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/engine/rpgmaker/detector"
)

func readReviewValidation(ctx context.Context, scope ReviewReadScope, head ReviewHead, result *ReviewDetail) error {
	current, err := projectReviewValidation(ctx, scope.Validation, head, result)
	if err != nil {
		return err
	}
	if current && head.ValidationID != nil {
		screenshot, found, err := scope.Media.RuntimeScreenshot(ctx, head.ItemID, *head.ValidationID)
		if err != nil {
			return fmt.Errorf("read review runtime screenshot: %w", err)
		}
		if found {
			result.RuntimeScreenshot = &screenshot
		}
	}
	if result.MultiDisc != nil && head.ValidationID != nil && !current {
		result.MultiDisc.CanAttachMissingDiscs = false
	}
	result.CanApprove = ReviewApproval(head.ContentKind, result.CanApprove, result.RuntimeScreenshot != nil)
	profile, found, err := scope.Validation.Profile(ctx, head.DraftID)
	if err != nil {
		return fmt.Errorf("read review RPG profile: %w", err)
	}
	if found {
		result.RPGMaker, err = ProjectReviewRPGMaker(profile)
	}
	return err
}

func projectReviewValidation(
	ctx context.Context,
	reader ReviewValidationReader,
	head ReviewHead,
	result *ReviewDetail,
) (bool, error) {
	if head.ValidationID == nil {
		return false, nil
	}
	current, err := NewReviewValidation(reader).Current(ctx, *head.ValidationID)
	if err != nil {
		return false, err
	}
	var dependency json.RawMessage
	if head.DependencyJSON != nil {
		dependency, err = reviewDocument(*head.DependencyJSON)
		if err != nil {
			return false, err
		}
	}
	ready := optionalTextValue(head.ValidationStatus) == "READY"
	result.Validation = &ReviewValidationView{
		ID: *head.ValidationID, Status: optionalTextValue(head.ValidationStatus),
		Current:            current && ready,
		CompatibilityCode:  optionalTextValue(head.CompatibilityCode),
		DependencySnapshot: dependency,
	}
	result.CanApprove = head.SelectedValidationID != nil && current && ready && head.Policy.Supports(head.ContentKind)
	return current, nil
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
