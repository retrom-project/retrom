package libraryimport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/contentcapability"
)

const prepublishGeneration = 4

const (
	validatorImportV4 = "import-source-validator-v4"
	validatorReviewV4 = "review-compatible-v4"
	validatorArcadeV4 = "arcade-source-validator-v4"
	validatorMultiV4  = "multi-disc-source-validator-v4"
)

type prepublishDigestInput struct {
	SchemaVersion             int             `json:"schemaVersion"`
	ValidatorVersion          string          `json:"validatorVersion"`
	SourceSnapshotID          string          `json:"sourceSnapshotId"`
	SourceManifestDigest      string          `json:"sourceManifestDigest"`
	ContentKind               string          `json:"contentKind"`
	TargetPlatformInstanceID  string          `json:"targetPlatformInstanceId"`
	PlatformInstanceVersion   int64           `json:"platformInstanceVersion"`
	CoreArtifactID            string          `json:"coreArtifactId"`
	CoreArtifactVersion       int64           `json:"coreArtifactVersion"`
	CompatibilityConfigDigest string          `json:"compatibilityConfigDigest"`
	DATVersionID              *string         `json:"datVersionId"`
	DefaultDOSEntry           *string         `json:"defaultDosEntry"`
	DependencySnapshot        json.RawMessage `json:"dependencySnapshot"`
	Status                    string          `json:"status"`
	CompatibilityCode         string          `json:"compatibilityCode"`
}

func prepublishDigest(input prepublishDigestInput) string {
	canonical, _ := json.Marshal(input)
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func prepublishDigestMatches(digest string, input prepublishDigestInput) bool {
	for _, version := range []string{validatorImportV4, validatorReviewV4, validatorArcadeV4, validatorMultiV4} {
		input.ValidatorVersion = version
		if subtle.ConstantTimeCompare([]byte(digest), []byte(prepublishDigest(input))) == 1 {
			return true
		}
	}
	return false
}

func compatibilityConfigDigest(compatibility string) string {
	digest := sha256.Sum256([]byte(compatibility))
	return hex.EncodeToString(digest[:])
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	valueCopy := value.String
	return &valueCopy
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	valueCopy := value
	return &valueCopy
}

func preparedGroupContentKind(group preparedGroup) string {
	if group.contentKind != "" {
		return group.contentKind
	}
	for _, source := range group.sources {
		if source.role == "DOS_SOURCE" {
			return "DOS_BUNDLE"
		}
	}
	return "SINGLE_FILE"
}

type reviewValidationEvidence struct {
	generation, platformVersion, artifactVersion                        int64
	sourceSnapshotID, targetID, coreID, artifactID, manifestDigest      string
	inputDigest, status, compatibilityCode, dependencyJSON              string
	validationDAT, validationDOS, draftDOS, activeDAT                   sql.NullString
	draftSnapshotID, draftTargetID, snapshotManifestDigest, contentKind string
	currentCoreID, currentArtifactID, compatibilityConfig               string
	currentPlatformVersion, currentArtifactVersion                      int64
}

func (service *Service) readReviewValidationEvidence(
	ctx context.Context,
	validationID string,
) (reviewValidationEvidence, error) {
	var value reviewValidationEvidence
	err := service.database.QueryRowContext(ctx, `
SELECT validation.prepublish_generation,validation.platform_instance_version,
validation.core_artifact_version,validation.source_snapshot_id,
validation.target_platform_instance_id,validation.core_id,validation.core_artifact_id,
validation.source_manifest_digest,validation.prepublish_input_digest,validation.status,
validation.compatibility_code,validation.dependency_snapshot_json,validation.dat_version_id,
validation.default_dos_entry,draft.default_dos_entry,
(SELECT active.id FROM dat_versions active
 WHERE active.core_artifact_id=artifact.id AND active.is_active=1),
draft.effective_source_snapshot_id,draft.target_platform_instance_id,
snapshot.source_manifest_digest,snapshot.content_kind,platform.default_core_id,
artifact.id,artifact.compatibility_json,platform.version,artifact.version
FROM import_item_core_validations validation
JOIN import_items item ON item.id=validation.import_item_id AND item.state='REVIEW_PENDING'
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN core_artifacts artifact ON artifact.core_id=platform.default_core_id AND artifact.selected_for_new_bindings=1
WHERE validation.id=?
`, validationID).Scan(
		&value.generation, &value.platformVersion, &value.artifactVersion,
		&value.sourceSnapshotID, &value.targetID, &value.coreID, &value.artifactID,
		&value.manifestDigest, &value.inputDigest, &value.status, &value.compatibilityCode,
		&value.dependencyJSON, &value.validationDAT, &value.validationDOS, &value.draftDOS,
		&value.activeDAT, &value.draftSnapshotID, &value.draftTargetID,
		&value.snapshotManifestDigest, &value.contentKind, &value.currentCoreID,
		&value.currentArtifactID, &value.compatibilityConfig,
		&value.currentPlatformVersion, &value.currentArtifactVersion,
	)
	if err != nil {
		return reviewValidationEvidence{}, fmt.Errorf("libraryimport/review validation evidence: %w", err)
	}
	return value, nil
}

func nullableStringsEqual(left, right sql.NullString) bool {
	return left.Valid == right.Valid && (!left.Valid || left.String == right.String)
}

func (value reviewValidationEvidence) currentInput() (prepublishDigestInput, bool) {
	current := value.generation == prepublishGeneration && value.sourceSnapshotID == value.draftSnapshotID &&
		value.targetID == value.draftTargetID && value.platformVersion == value.currentPlatformVersion &&
		value.coreID == value.currentCoreID && value.artifactID == value.currentArtifactID &&
		value.artifactVersion == value.currentArtifactVersion && value.manifestDigest == value.snapshotManifestDigest &&
		nullableStringsEqual(value.validationDAT, value.activeDAT) &&
		nullableStringsEqual(value.validationDOS, value.draftDOS) &&
		contentcapability.SupportsContentKind(value.compatibilityConfig, value.contentKind)
	if !current {
		return prepublishDigestInput{}, false
	}
	return prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: value.sourceSnapshotID,
		SourceManifestDigest: value.manifestDigest, ContentKind: value.contentKind,
		TargetPlatformInstanceID: value.targetID, PlatformInstanceVersion: value.platformVersion,
		CoreArtifactID: value.artifactID, CoreArtifactVersion: value.artifactVersion,
		CompatibilityConfigDigest: compatibilityConfigDigest(value.compatibilityConfig),
		DATVersionID:              nullStringPointer(value.validationDAT),
		DefaultDOSEntry:           nullStringPointer(value.validationDOS),
		DependencySnapshot:        json.RawMessage(value.dependencyJSON), Status: value.status,
		CompatibilityCode: value.compatibilityCode,
	}, true
}

func (service *Service) ReviewValidationCurrent(ctx context.Context, validationID string) (bool, error) {
	value, err := service.readReviewValidationEvidence(ctx, validationID)
	if err != nil {
		return false, err
	}
	input, current := value.currentInput()
	if !current {
		return false, nil
	}
	return prepublishDigestMatches(value.inputDigest, input), nil
}
