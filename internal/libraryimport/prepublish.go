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

type prepublishDigestInput struct {
	SchemaVersion            int             `json:"schemaVersion"`
	SourceSnapshotID         string          `json:"sourceSnapshotId"`
	SourceManifestDigest     string          `json:"sourceManifestDigest"`
	ContentKind              string          `json:"contentKind"`
	TargetPlatformInstanceID string          `json:"targetPlatformInstanceId"`
	ProviderID               string          `json:"providerId"`
	TargetID                 string          `json:"targetId"`
	ContentPolicyDigest      string          `json:"contentPolicyDigest"`
	DATVersionID             *string         `json:"datVersionId"`
	DefaultDOSEntry          *string         `json:"defaultDosEntry"`
	DependencySnapshot       json.RawMessage `json:"dependencySnapshot"`
	Status                   string          `json:"status"`
	CompatibilityCode        string          `json:"compatibilityCode"`
}

func prepublishDigest(input prepublishDigestInput) string {
	canonical, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func prepublishDigestMatches(digest string, input prepublishDigestInput) bool {
	return len(digest) == 64 && subtle.ConstantTimeCompare([]byte(digest), []byte(prepublishDigest(input))) == 1
}

func compatibilityConfigDigest(compatibility string) string {
	var value any
	if err := json.Unmarshal([]byte(compatibility), &value); err != nil {
		return ""
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

// Validation depends on the selected content policy, not unrelated accepted kinds.
func validationPolicyDigest(compatibility, contentKind string) string {
	if !contentcapability.SupportsContentKind(compatibility, contentKind) {
		return ""
	}
	var policy struct {
		MultiDisc any `json:"multiDisc"`
	}
	if err := json.Unmarshal([]byte(compatibility), &policy); err != nil {
		return ""
	}
	if contentKind != contentcapability.ModeMultiDisc {
		policy.MultiDisc = nil
	}
	canonical, err := json.Marshal(struct {
		ContentKind string `json:"contentKind"`
		MultiDisc   any    `json:"multiDisc"`
	}{ContentKind: contentKind, MultiDisc: policy.MultiDisc})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
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
	platformVersion                                                    int64
	sourceSnapshotID, platformInstanceID, coreID, providerID, targetID string
	manifestDigest, inputDigest, status, compatibilityCode             string
	dependencyJSON                                                     string
	validationDAT, validationDOS, draftDOS, activeDAT                  sql.NullString
	draftSnapshotID, draftPlatformInstanceID, snapshotManifestDigest   string
	contentKind, contentPolicyJSON                                     string
	currentCoreID                                                      string
	currentPlatformVersion                                             int64
}

func (service *Service) readReviewValidationEvidence(
	ctx context.Context,
	validationID string,
) (reviewValidationEvidence, error) {
	var value reviewValidationEvidence
	err := service.database.QueryRowContext(ctx, `
SELECT validation.platform_instance_version,
validation.source_snapshot_id,
validation.target_platform_instance_id,validation.core_id,validation.provider_id,validation.target_id,
validation.source_manifest_digest,validation.prepublish_input_digest,validation.status,
validation.compatibility_code,validation.dependency_snapshot_json,validation.dat_version_id,
validation.default_dos_entry,draft.default_dos_entry,
(SELECT active.id FROM dat_versions active
 WHERE active.provider_id=validation.provider_id AND active.target_id=validation.target_id AND active.is_active=1),
draft.effective_source_snapshot_id,draft.target_platform_instance_id,
snapshot.source_manifest_digest,snapshot.content_kind,platform.default_core_id,platform.version,
json_object(
 'schemaVersion',1,
 'supportedContentKinds',json((SELECT json_group_array(content_kind) FROM (
   SELECT content_kind FROM runtime_binding_content_kinds kinds
   WHERE kinds.binding_id=binding.binding_id ORDER BY content_kind
 ))),
 'multiDisc',CASE WHEN EXISTS(
   SELECT 1 FROM runtime_binding_content_kinds kinds
   WHERE kinds.binding_id=binding.binding_id AND kinds.content_kind='MULTI_DISC'
 ) THEN json_object('maxDiscs',8,'maxTotalBytes',1073741824,'delivery','EAGER_EXTERNAL_FILES') ELSE NULL END
)
FROM import_item_core_validations validation
JOIN import_items item ON item.id=validation.import_item_id AND item.state='REVIEW_PENDING'
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
LEFT JOIN rpgmaker_review_profiles rpg_profile ON rpg_profile.review_draft_id=draft.id
JOIN runtime_targets target ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
 AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=platform.platform_id
WHERE validation.id=?
`, validationID).Scan(
		&value.platformVersion,
		&value.sourceSnapshotID, &value.platformInstanceID, &value.coreID,
		&value.providerID, &value.targetID,
		&value.manifestDigest, &value.inputDigest, &value.status, &value.compatibilityCode,
		&value.dependencyJSON, &value.validationDAT, &value.validationDOS, &value.draftDOS,
		&value.activeDAT, &value.draftSnapshotID, &value.draftPlatformInstanceID,
		&value.snapshotManifestDigest, &value.contentKind, &value.currentCoreID,
		&value.currentPlatformVersion, &value.contentPolicyJSON,
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
	current := value.sourceSnapshotID == value.draftSnapshotID &&
		value.platformInstanceID == value.draftPlatformInstanceID &&
		value.coreID == value.currentCoreID && value.manifestDigest == value.snapshotManifestDigest &&
		nullableStringsEqual(value.validationDAT, value.activeDAT) &&
		nullableStringsEqual(value.validationDOS, value.draftDOS) &&
		contentcapability.SupportsContentKind(value.contentPolicyJSON, value.contentKind)
	if !current {
		return prepublishDigestInput{}, false
	}
	return prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: value.sourceSnapshotID,
		SourceManifestDigest: value.manifestDigest, ContentKind: value.contentKind,
		TargetPlatformInstanceID: value.platformInstanceID,
		ProviderID:               value.providerID, TargetID: value.targetID,
		ContentPolicyDigest: validationPolicyDigest(value.contentPolicyJSON, value.contentKind),
		DATVersionID:        nullStringPointer(value.validationDAT),
		DefaultDOSEntry:     nullStringPointer(value.validationDOS),
		DependencySnapshot:  json.RawMessage(value.dependencyJSON), Status: value.status,
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
