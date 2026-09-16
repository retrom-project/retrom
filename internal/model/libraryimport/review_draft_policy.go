package libraryimport

import "encoding/json"

// ValidateDraftAssetSelection validates the pure constraints of an asset
// patch: screenshot count limit, duplicate detection, and cover/uploaded
// cover mutual exclusion. The caller must separately validate that each
// individual ID actually exists and is eligible via database queries.
func ValidateDraftAssetSelection(
	screenshotAssetIDs []string,
	coverID, uploadedCoverID *string,
) error {
	if len(screenshotAssetIDs) > 32 {
		return ErrInvalid
	}
	selected := make(map[string]struct{}, len(screenshotAssetIDs))
	for _, assetID := range screenshotAssetIDs {
		if _, duplicate := selected[assetID]; duplicate {
			return ErrInvalid
		}
		selected[assetID] = struct{}{}
	}
	if coverID != nil && uploadedCoverID != nil {
		return ErrInvalid
	}
	return nil
}

// RPGOverrideFacts holds the database-loaded facts required by
// ValidateRPGOverride.
type RPGOverrideFacts struct {
	IsRPG                 bool
	TargetOrDOSChanged    bool
	SelfContainedOverride *bool
	Generation            string
}

// ValidateRPGOverride validates the pure business rules for RPG self-contained
// override changes. It returns nil when the override is valid, ErrInvalid when
// the combination is forbidden. The caller reads generation from the database
// and passes it here.
func ValidateRPGOverride(facts RPGOverrideFacts) error {
	if !facts.IsRPG {
		if facts.SelfContainedOverride != nil {
			return ErrInvalid
		}
		return nil
	}
	if facts.TargetOrDOSChanged {
		return ErrInvalid
	}
	if facts.SelfContainedOverride == nil {
		return nil
	}
	if *facts.SelfContainedOverride &&
		(facts.Generation == "RPGMV" || facts.Generation == "RPGMZ") {
		return ErrInvalid
	}
	return nil
}

// ValidateDraftTargetChange checks whether a platform change is allowed.
// It requires that both platforms are the same; a cross-platform change
// requires a full reimport.
func ValidateDraftTargetChange(currentPlatformID, targetPlatformID string) error {
	if currentPlatformID != targetPlatformID {
		return ErrReimportRequiredPlatformChange
	}
	return nil
}

// ValidateDraftDOSEntry checks whether the given entry exists in the allowed
// set. allowedEntries is the set of enabled DOS entries for the item, loaded
// by the caller from the database.
func ValidateDraftDOSEntry(entry string, allowedEntries map[string]struct{}) error {
	if _, ok := allowedEntries[entry]; !ok {
		return ErrInvalid
	}
	return nil
}

// ValidationCreateMatchesFacts holds facts for pure validation create matching.
type ValidationCreateMatchesFacts struct {
	ItemID                   string
	TargetID                 string
	EffectiveSnapshotID      string
	DOSEntry                 *string
	Guard                    ReviewValidationGuard
	ExpectedPrepublishDigest string
	ValidationID             string
}

// ValidateValidationCreate checks that a proposed ValidationCreate is
// consistent with the current plan state, without any I/O.
func ValidateValidationCreate(
	create ReviewValidationRefreshCreate,
	facts ValidationCreateMatchesFacts,
) bool {
	if create.ID == "" || create.ItemID != facts.ItemID ||
		create.TargetPlatformInstanceID != facts.TargetID {
		return false
	}
	if create.SourceSnapshotID != facts.EffectiveSnapshotID ||
		!sameNullableStr(create.DefaultDOSEntry, facts.DOSEntry) {
		return false
	}
	guard := facts.Guard
	if create.PlatformInstanceVersion != guard.PlatformInstanceVersion ||
		create.CoreID != guard.CoreID {
		return false
	}
	if create.ProviderID != guard.ProviderID || create.TargetID != guard.TargetID {
		return false
	}
	if create.SourceManifestDigest != guard.SourceManifestDigest ||
		!sameNullableStr(create.DATVersionID, guard.DATVersionID) {
		return false
	}
	if create.PrepublishInputDigest != facts.ExpectedPrepublishDigest {
		return false
	}
	return (create.Status == "READY") == (facts.ValidationID == create.ID)
}

// ExistingValidationMatchesFacts holds DB-loaded facts for existing
// validation matching.
type ExistingValidationMatchesFacts struct {
	TargetID                 string
	EffectiveSnapshotID      string
	ExpectedPrepublishDigest string
	Guard                    ReviewValidationGuard
}

// ValidateExistingValidation checks that a loaded validation row matches the
// plan expectations, purely from values.
func ValidateExistingValidation(
	targetID, snapshotID, status, coreID, providerID,
	runtimeTargetID, sourceManifestDigest string,
	datVersionID, dosEntry *string,
	prepublishInputDigest, dependencySnapshot, compatibilityCode string,
	facts ExistingValidationMatchesFacts,
) bool {
	if targetID != facts.TargetID ||
		snapshotID != facts.EffectiveSnapshotID || status != "READY" {
		return false
	}
	guard := facts.Guard
	if coreID != guard.CoreID || providerID != guard.ProviderID {
		return false
	}
	if runtimeTargetID != guard.TargetID ||
		sourceManifestDigest != guard.SourceManifestDigest {
		return false
	}
	if !sameNullableStr(datVersionID, guard.DATVersionID) ||
		!sameNullableStr(dosEntry, guard.DefaultDOSEntry) {
		return false
	}
	if prepublishInputDigest != facts.ExpectedPrepublishDigest {
		return false
	}
	return PrepublishDigestMatches(prepublishInputDigest, PrepublishDigestInput{
		SchemaVersion:            1,
		SourceSnapshotID:         guard.SourceSnapshotID,
		SourceManifestDigest:     guard.SourceManifestDigest,
		ContentKind:              guard.ContentKind,
		TargetPlatformInstanceID: guard.TargetPlatformInstanceID,
		ProviderID:               guard.ProviderID,
		TargetID:                 guard.TargetID,
		ContentPolicyDigest:      guard.ContentPolicyDigest,
		DATVersionID:             guard.DATVersionID,
		DefaultDOSEntry:          guard.DefaultDOSEntry,
		DependencySnapshot:       json.RawMessage(dependencySnapshot),
		Status:                   status,
		CompatibilityCode:        compatibilityCode,
	})
}

func sameNullableStr(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
