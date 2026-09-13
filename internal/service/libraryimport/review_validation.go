package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
)

type ReviewValidation struct{ reader ReviewValidationReader }

func NewReviewValidation(reader ReviewValidationReader) *ReviewValidation {
	return &ReviewValidation{reader: reader}
}

func (value ReviewValidationEvidence) CurrentInput() (PrepublishDigestInput, bool) {
	current := value.SourceSnapshotID == value.DraftSnapshotID &&
		value.PlatformInstanceID == value.DraftPlatformInstanceID &&
		value.CoreID == value.CurrentCoreID && value.ManifestDigest == value.SnapshotManifestDigest &&
		equalOptionalText(value.ValidationDAT, value.ActiveDAT) && equalOptionalText(value.ValidationDOS, value.DraftDOS) &&
		value.ContentPolicy.Supports(value.ContentKind)
	if !current {
		return PrepublishDigestInput{}, false
	}
	return PrepublishDigestInput{
		SchemaVersion:        1,
		SourceSnapshotID:     value.SourceSnapshotID,
		SourceManifestDigest: value.ManifestDigest,
		ContentKind:          value.ContentKind,

		TargetPlatformInstanceID: value.PlatformInstanceID, ProviderID: value.ProviderID, TargetID: value.TargetID,
		ContentPolicyDigest: value.ContentPolicy.DigestFor(value.ContentKind),
		DATVersionID:        value.ValidationDAT,
		DefaultDOSEntry:     value.ValidationDOS,

		DependencySnapshot: json.RawMessage(value.DependencyJSON),
		Status:             value.Status,
		CompatibilityCode:  value.CompatibilityCode,
	}, true
}

func equalOptionalText(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
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
