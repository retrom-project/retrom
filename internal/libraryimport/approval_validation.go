package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
	"retrom/internal/multidisc"
	"retrom/internal/payloadrelease"
)

// Resolution rows and aggregate repair must commit as one auditable operation.
func recordImportReconfiguration(
	ctx context.Context,
	transaction *sql.Tx,
	input reconfigurationInput,
	replacementImportJobID string,
	now int64,
) error {
	actor := reviewActor(ctx)
	for _, uploadFileID := range input.sourceFileIDs {
		result, err := transaction.ExecContext(ctx, `
INSERT INTO import_job_file_resolutions(import_job_id,
upload_file_id,
action,
replacement_import_job_id,
actor_kind,
actor_user_id,
actor_label,
created_at_ms)
SELECT f.import_job_id,
f.upload_file_id,
'RECONFIGURED',
?,
?,
?,
?,
?
FROM import_job_files f
LEFT JOIN import_job_file_resolutions resolution
ON resolution.import_job_id=f.import_job_id
AND resolution.upload_file_id=f.upload_file_id
WHERE f.import_job_id=?
AND f.upload_file_id=?
AND f.disposition='REJECTED'
AND resolution.upload_file_id IS NULL
`, replacementImportJobID, actor.Kind, actor.UserID, actor.Label, now, input.sourceImportJobID, uploadFileID)
		if err != nil {
			return fmt.Errorf("libraryimport/reconfigure: %w", err)
		}
		inserted, err := result.RowsAffected()
		if err != nil || inserted != 1 {
			return ErrInvalid
		}
	}
	resolved := int64(len(input.sourceFileIDs))
	result, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET resolved_rejected_file_count=resolved_rejected_file_count+?,
state=CASE
  WHEN queued_item_count>0 OR running_item_count>0 THEN 'RUNNING'
  WHEN failed_item_count>0 OR rejected_file_count>resolved_rejected_file_count+? THEN 'PARTIAL_FAILURE'
  WHEN review_pending_item_count>0 THEN 'REVIEW_PENDING'
  ELSE 'COMPLETED'
END,
completed_at_ms=CASE
  WHEN queued_item_count=0
   AND running_item_count=0
   AND failed_item_count=0
   AND rejected_file_count=resolved_rejected_file_count+?
   AND review_pending_item_count=0 THEN ?
  ELSE NULL
END,
version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
AND state='PARTIAL_FAILURE'
AND resolved_rejected_file_count+?<=rejected_file_count
`, resolved, resolved, resolved, now, now, input.sourceImportJobID, input.sourceVersion, resolved)
	if err != nil {
		return fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs
SET reconfigured_from_import_job_id=?
WHERE id=?
`, input.sourceImportJobID, replacementImportJobID); err != nil {
		return fmt.Errorf("libraryimport/reconfigure: %w", err)
	}
	if _, err := payloadrelease.ScheduleTerminalImportJob(ctx, transaction, input.sourceImportJobID, now); err != nil {
		return fmt.Errorf("libraryimport/reconfigure source release: %w", err)
	}
	return nil
}

func prepareStaticBIOSDependencies(
	ctx context.Context,
	transaction *sql.Tx,
	providerID, targetID, platformID string,
	groups []preparedGroup,
) error {
	if skipsStaticBIOS(platformID) {
		return nil
	}
	for index := range groups {
		logicalName := ""
		for _, source := range groups[index].sources {
			if source.role == "CONTENT" || source.role == "DISC" {
				logicalName = source.logicalName
				break
			}
		}
		// DOS imports consist exclusively of DOS_SOURCE entries. The selected
		// default program is the stable content identity used for conditional
		// dependency evaluation, just as CONTENT/DISC is for other platforms.
		if logicalName == "" && platformID == "dos" {
			logicalName = groups[index].defaultDOSEntry
		}
		if logicalName == "" {
			return ErrInvalid
		}
		snapshot, status, code, err := corevalidation.ResolveBIOS(
			ctx, transaction, providerID, targetID, logicalName,
		)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		snapshot.MultiDisc = groups[index].multiDependency
		snapshotJSON, err := snapshot.JSON()
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		if groups[index].compatibilityCode != "MULTI_DISC_FILE_MISSING" {
			groups[index].validationStatus = status
			groups[index].compatibilityCode = code
		}
		groups[index].dependencySnapshot = string(snapshotJSON)
		for _, dependency := range snapshot.BIOS {
			if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
				groups[index].validationFiles = append(groups[index].validationFiles, preparedValidationFile{
					role: "BIOS_BUNDLE", logicalName: dependency.LogicalName,
					blobID: *dependency.BlobID, sortOrder: len(groups[index].validationFiles),
				})
			}
		}
	}
	return nil
}

func skipsStaticBIOS(platformID string) bool {
	switch platformID {
	case "arcade", "rpgmaker", "ons", "kirikiri", "butterscotch", "tyranoscript":
		return true
	default:
		return false
	}
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableIntPointer(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

type Approved struct {
	GameID  string `json:"gameId"`
	EventID string `json:"reviewEventId"`
	Status  string `json:"status"`
}

type ApprovalDecision struct {
	Reason              *string
	DuplicatePolicy     string
	AcknowledgedGameIDs []string
	SourceKind          string
	SourceRefID         string
	ExternalAssets      []ExternalAsset
}

type approvalOptions struct {
	strictReady              bool
	expectedValidationID     string
	expectedSourceSnapshotID string
	bulkApprovalID           string
	beforeCommit             func(context.Context, *sql.Tx, Approved) error
}

type ExternalAsset struct {
	Kind      string
	BlobID    string
	MediaType string
	WidthPX   *int64
	HeightPX  *int64
}

func multiDiscApprovalEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	sourceSnapshotID string,
) (int, int64, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT entry.ordinal,entry.state,entry.blob_id,entry.source_logical_name,
       file.blob_id,file.logical_name,file.sort_order,blob.size_bytes
FROM import_item_multidisc_entries entry
LEFT JOIN import_item_source_snapshot_files file
  ON file.source_snapshot_id=entry.source_snapshot_id
 AND file.role='DISC' AND file.sort_order=entry.ordinal
LEFT JOIN blobs blob ON blob.id=entry.blob_id
WHERE entry.source_snapshot_id=?
ORDER BY entry.ordinal
`, sourceSnapshotID)
	if err != nil {
		return 0, 0, fmt.Errorf("libraryimport/approve multi-disc: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	discCount := 0
	var totalSize int64
	for rows.Next() {
		var ordinal, sortOrder int
		var state, entryBlobID, entryLogicalName, fileBlobID, fileLogicalName string
		var size int64
		if err := rows.Scan(
			&ordinal, &state, &entryBlobID, &entryLogicalName,
			&fileBlobID, &fileLogicalName, &sortOrder, &size,
		); err != nil {
			return 0, 0, ErrInvalid
		}
		if ordinal != discCount || sortOrder != ordinal || state != "PRESENT" ||
			entryBlobID != fileBlobID || entryLogicalName != fileLogicalName || size < 8 {
			return 0, 0, ErrInvalid
		}
		totalSize += size
		discCount++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("libraryimport/approve multi-disc: %w", err)
	}
	return discCount, totalSize, nil
}

func validateMultiDiscSourceCounts(
	ctx context.Context,
	transaction *sql.Tx,
	sourceSnapshotID string,
	discCount int,
) error {
	var playlistCount, sourceDiscCount, sourceCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FILTER(WHERE role='PLAYLIST_SOURCE'),
       count(*) FILTER(WHERE role='DISC'),count(*)
FROM import_item_source_snapshot_files WHERE source_snapshot_id=?
`, sourceSnapshotID).Scan(&playlistCount, &sourceDiscCount, &sourceCount); err != nil ||
		playlistCount != 1 || sourceDiscCount != discCount || sourceCount != discCount+1 {
		return ErrInvalid
	}
	return nil
}

func validateMultiDiscApproval(
	ctx context.Context,
	transaction *sql.Tx,
	sourceSnapshotID, validationID, platformID string,
	contentPolicy contentcapability.Policy,
	snapshot corevalidation.Snapshot,
) error {
	capabilities := contentcapability.Resolve(platformID, true, true, contentPolicy)
	if capabilities.MultiDisc == nil || snapshot.MultiDisc == nil ||
		len(snapshot.MultiDisc.MissingEntries) != 0 {
		return ErrInvalid
	}
	discCount, totalSize, err := multiDiscApprovalEvidence(ctx, transaction, sourceSnapshotID)
	if err != nil {
		return err
	}
	if discCount < 2 || discCount > capabilities.MultiDisc.MaxDiscs ||
		discCount != snapshot.MultiDisc.DiscCount || totalSize > capabilities.MultiDisc.MaxTotalBytes {
		return ErrInvalid
	}
	if err := validateMultiDiscSourceCounts(ctx, transaction, sourceSnapshotID, discCount); err != nil {
		return err
	}
	var canonicalCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role='MULTI_DISC_PLAYLIST'
`, validationID).Scan(&canonicalCount); err != nil || canonicalCount != 1 {
		return ErrInvalid
	}
	return nil
}

func validateCurrentApprovalSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	sourceSnapshotID, validationID, platformID, providerID, targetID string,
	contentPolicy contentcapability.Policy,
	contentKind string,
	validationSnapshot corevalidation.Snapshot,
	frozenJSON string,
) error {
	contentLogicalName, err := snapshotContentLogicalName(ctx, transaction, sourceSnapshotID)
	if err != nil {
		return err
	}
	currentSnapshot, validationStatus, _, err := corevalidation.ResolveBIOS(
		ctx, transaction, providerID, targetID, contentLogicalName,
	)
	if err != nil || validationStatus != "READY" {
		return ErrInvalid
	}
	currentSnapshot.MultiDisc = validationSnapshot.MultiDisc
	currentJSON, err := currentSnapshot.JSON()
	if err != nil || string(currentJSON) != frozenJSON {
		return ErrInvalid
	}
	if contentKind != multidisc.ContentKind {
		return nil
	}
	return validateMultiDiscApproval(
		ctx, transaction, sourceSnapshotID, validationID, platformID, contentPolicy, validationSnapshot,
	)
}

func validArcadeApprovalDependencyState(state string) bool {
	return state == "SATISFIED_BY_CONTENT" || state == "SATISFIED_EXTERNAL" || state == "HASH_WARNING"
}

func equalArcadeRequirementNames(requirements []arcadeROMRequirement, frozen []string) bool {
	if len(requirements) != len(frozen) {
		return false
	}
	for index := range requirements {
		if requirements[index].name != frozen[index] {
			return false
		}
	}
	return true
}

func arcadeDependencyValidationRole(kind string) string {
	if kind == "PARENT" {
		return "PARENT"
	}
	if kind == "BIOS_OR_BASE" {
		return "BIOS_BUNDLE"
	}
	return ""
}

func validateArcadeExternalDependencyFile(
	ctx context.Context,
	transaction *sql.Tx,
	validationID string,
	dependency arcadeDraftDependency,
) error {
	role := arcadeDependencyValidationRole(dependency.Kind)
	if role == "" {
		return ErrInvalid
	}
	var count int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role=? AND logical_name=?
`, validationID, role, dependency.ExpectedLogicalName).Scan(&count); err != nil {
		return fmt.Errorf("libraryimport/approve arcade dependency: %w", err)
	}
	if count != 1 {
		return ErrInvalid
	}
	return nil
}

func (service *Service) validateCurrentArcadeApprovalDependency(
	ctx context.Context,
	transaction *sql.Tx,
	validationID, datVersionID string,
	dependency arcadeDraftDependency,
) error {
	if !validArcadeApprovalDependencyState(dependency.State) {
		return ErrInvalid
	}
	requirements, hasDisk, err := arcadeRequirementsWithQueryer(
		ctx, transaction, datVersionID, dependency.Machine,
	)
	if err != nil || hasDisk || !equalArcadeRequirementNames(requirements, dependency.RequiredEntries) {
		return ErrInvalid
	}
	if dependency.State == "SATISFIED_BY_CONTENT" {
		return nil
	}
	return validateArcadeExternalDependencyFile(ctx, transaction, validationID, dependency)
}

func (service *Service) validateCurrentArcadeApprovalSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	validationID, frozenJSON string,
) error {
	frozen, valid := parseArcadeDraftSnapshot(frozenJSON)
	if !valid || len(frozen.MissingEntries) != 0 ||
		len(frozen.MismatchedEntries) != 0 {
		return ErrInvalid
	}
	canonical, err := service.canonicalArcadeSnapshotWithQueryer(ctx, transaction, frozenJSON)
	if err != nil || len(canonical.Dependencies) != len(projectedClosureDependencies(canonical.Closure)) {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(canonical.Dependencies))
	for _, dependency := range canonical.Dependencies {
		key := dependency.Kind + "\x00" + dependency.Machine
		if _, duplicate := seen[key]; duplicate {
			return ErrInvalid
		}
		seen[key] = struct{}{}
		if err := service.validateCurrentArcadeApprovalDependency(
			ctx, transaction, validationID, canonical.DatVersionID, dependency,
		); err != nil {
			return err
		}
	}
	frozenCanonical, frozenErr := json.Marshal(frozen)
	canonicalJSON, canonicalErr := json.Marshal(canonical)
	if frozenErr != nil || canonicalErr != nil || !bytes.Equal(frozenCanonical, canonicalJSON) {
		return ErrInvalid
	}
	return nil
}

func projectedClosureDependencies(raw json.RawMessage) []arcadeClosureNode {
	var nodes []arcadeClosureNode
	if json.Unmarshal(raw, &nodes) != nil {
		return nil
	}
	dependencies := make([]arcadeClosureNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind != "CONTENT" {
			dependencies = append(dependencies, node)
		}
	}
	return dependencies
}

func (service *Service) validateCurrentApprovalDependencySnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	sourceSnapshotID, validationID, platformID, providerID, targetID string,
	contentPolicy contentcapability.Policy,
	contentKind, frozenJSON string,
) error {
	snapshot, err := corevalidation.ParseSnapshot(frozenJSON)
	if err == nil {
		return validateCurrentApprovalSnapshot(
			ctx, transaction, sourceSnapshotID, validationID, platformID, providerID, targetID,
			contentPolicy, contentKind, snapshot, frozenJSON,
		)
	}
	if platformID != "arcade" || contentKind != "SINGLE_FILE" {
		return ErrInvalid
	}
	return service.validateCurrentArcadeApprovalSnapshot(ctx, transaction, validationID, frozenJSON)
}

func screenshotOverrideRuntimeSnapshot(snapshot corevalidation.Snapshot) corevalidation.Snapshot {
	filtered := make([]corevalidation.BIOSDependency, 0, len(snapshot.BIOS))
	for _, dependency := range snapshot.BIOS {
		if dependency.DeliveryKind != "EXTERNAL_FILE" {
			filtered = append(filtered, dependency)
			continue
		}
		if dependency.EmulatorPath == nil || dependency.BlobID == nil || dependency.InstallationStatus == nil {
			continue
		}
		if *dependency.InstallationStatus == "MATCHED" || *dependency.InstallationStatus == "HASH_WARNING" {
			filtered = append(filtered, dependency)
		}
	}
	snapshot.BIOS = filtered
	return snapshot
}

type approvalValidationDigestInput struct {
	VariantID, ContentID, ContentKind, ProviderID, TargetID string
	ContentPolicy                                           contentcapability.Policy
	DATID                                                   sql.NullString
	ValidationID                                            string
	Snapshot                                                corevalidation.Snapshot
	SnapshotValid                                           bool
}

func approvalValidationInputDigest(input approvalValidationDigestInput) (string, error) {
	if !input.SnapshotValid {
		if input.ContentKind != contentcapability.ModeRPGMakerProject {
			validationDigest := sha256.Sum256([]byte(input.ValidationID))
			return hex.EncodeToString(validationDigest[:]), nil
		}
		input.Snapshot = corevalidation.Snapshot{
			SchemaVersion: corevalidation.SnapshotSchemaVersion, Kind: corevalidation.SnapshotKindStatic,
			BIOS: []corevalidation.BIOSDependency{},
		}
	}
	if input.ContentKind != multidisc.ContentKind {
		digest, err := corevalidation.ProviderValidationInputDigest(
			input.ProviderID, input.TargetID, input.ContentID, input.DATID, input.Snapshot,
		)
		if err != nil {
			return "", fmt.Errorf("libraryimport/service: %w", err)
		}
		return digest, nil
	}
	if input.Snapshot.MultiDisc == nil {
		return "", ErrInvalid
	}
	biosDigest, err := corevalidation.BIOSDependencyDigest(input.Snapshot)
	if err != nil {
		return "", ErrInvalid
	}
	digest, err := corevalidation.MultiDiscValidationInputDigest(corevalidation.MultiDiscValidationInput{
		GameVariantID: input.VariantID, GameID: input.ContentID,
		ContentKind: input.ContentKind, ProviderID: input.ProviderID, TargetID: input.TargetID,
		ContentPolicySHA256: input.ContentPolicy.Digest(),
		DATVersionID:        input.DATID, BIOSDependencySHA256: biosDigest,
		OrderedDiscSHA256:       input.Snapshot.MultiDisc.OrderedDiscSHA256,
		CanonicalPlaylistSHA256: input.Snapshot.MultiDisc.CanonicalPlaylistSHA256,
	})
	if err != nil {
		return "", fmt.Errorf("libraryimport/service: %w", err)
	}
	return digest, nil
}
