package libraryimport

import "encoding/json"

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
