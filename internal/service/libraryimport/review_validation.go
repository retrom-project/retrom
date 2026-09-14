package libraryimport

import (
	"context"
	"fmt"
)

type ReviewValidation struct{ reader ReviewValidationReader }

func NewReviewValidation(reader ReviewValidationReader) *ReviewValidation {
	return &ReviewValidation{reader: reader}
}

func (service *ReviewValidation) Current(ctx context.Context, validationID string) (bool, error) {
	evidence, err := service.reader.Evidence(ctx, validationID)
	if err != nil {
		return false, fmt.Errorf("read review validation evidence: %w", err)
	}
	input, current := evidence.CurrentInput()
	if !current || !PrepublishDigestMatches(evidence.InputDigest, input) {
		return false, nil
	}
	if evidence.ContentKind != "RPG_MAKER_PROJECT" {
		return true, nil
	}
	profile, found, err := service.reader.Profile(ctx, evidence.DraftID)
	if err != nil {
		return false, fmt.Errorf("read current RPG profile: %w", err)
	}
	if !found {
		return false, ErrInvalid
	}
	dependencies, err := ResolveRPGReviewDependencies(profile)
	if err != nil {
		return false, err
	}
	return dependencies.SnapshotJSON == evidence.DependencyJSON && dependencies.Status == evidence.Status &&
		dependencies.Code == evidence.CompatibilityCode && dependencies.Digest == profile.DependencySHA256, nil
}
