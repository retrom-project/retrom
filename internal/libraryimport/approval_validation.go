package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	"retrom/internal/dbexec"
	validationpersistence "retrom/internal/persistence/corevalidation"
	librarypersistence "retrom/internal/persistence/libraryimport"
	validationservice "retrom/internal/service/corevalidation"
	application "retrom/internal/service/libraryimport"

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
		for _, source := range groups[index].Sources {
			if source.Role == "CONTENT" || source.Role == "DISC" {
				logicalName = source.LogicalName
				break
			}
		}
		// DOS imports consist exclusively of DOS_SOURCE entries. The selected
		// default program is the stable content identity used for conditional
		// dependency evaluation, just as CONTENT/DISC is for other platforms.
		if logicalName == "" && platformID == "dos" {
			logicalName = groups[index].DefaultDOSEntry
		}
		if logicalName == "" {
			return ErrInvalid
		}
		snapshot, status, code, err := validationservice.New(
			validationpersistence.New(
				transaction,
			),
		).ResolveBIOS(
			ctx,
			providerID,
			targetID,
			logicalName,
		)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		snapshot.MultiDisc = groups[index].MultiDependency
		snapshotJSON, err := snapshot.JSON()
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		if groups[index].CompatibilityCode != "MULTI_DISC_FILE_MISSING" {
			groups[index].ValidationStatus = status
			groups[index].CompatibilityCode = code
		}
		groups[index].DependencySnapshot = string(snapshotJSON)
		for _, dependency := range snapshot.BIOS {
			if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
				groups[index].ValidationFiles = append(groups[index].ValidationFiles, preparedValidationFile{
					Role: "BIOS_BUNDLE", LogicalName: dependency.LogicalName,
					BlobID: *dependency.BlobID, SortOrder: len(groups[index].ValidationFiles),
				})
			}
		}
	}
	return nil
}

func skipsStaticBIOS(platformID string) bool {
	switch platformID {
	case "cavestory", "arcade", "rpgmaker", "ons", "kirikiri", "butterscotch", "tyranoscript", "scummvm":
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

func nullableInt(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

type (
	Approved         = application.ReviewApproved
	ApprovalDecision = application.ReviewApprovalDecision
	ExternalAsset    = application.ApprovalExternalAsset
)

func (service *Service) validateCurrentApprovalDependencySnapshot(
	ctx context.Context, transaction *sql.Tx, sourceSnapshotID, validationID, platformID, providerID,
	targetID string,
	policy contentcapability.Policy, contentKind, frozenJSON string,
) error {
	err := application.ValidateApprovalDependencies(ctx,
		librarypersistence.BindApprovalDependencies(transaction), application.ApprovalDependencyInput{
			SnapshotID: sourceSnapshotID, ValidationID: validationID, PlatformID: platformID,
			ProviderID: providerID, TargetID: targetID,
			Policy: policy, ContentKind: contentKind, DependencyJSON: frozenJSON,
		})
	if err != nil {
		return fmt.Errorf("validate approval dependencies: %w", err)
	}
	return nil
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
			input.ProviderID, input.TargetID, input.ContentID, dbexec.StringPointer(input.DATID),
			input.Snapshot,
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
		DATVersionID:        dbexec.StringPointer(input.DATID), BIOSDependencySHA256: biosDigest,
		OrderedDiscSHA256:       input.Snapshot.MultiDisc.OrderedDiscSHA256,
		CanonicalPlaylistSHA256: input.Snapshot.MultiDisc.CanonicalPlaylistSHA256,
	})
	if err != nil {
		return "", fmt.Errorf("libraryimport/service: %w", err)
	}
	return digest, nil
}
