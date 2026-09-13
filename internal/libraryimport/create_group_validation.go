package libraryimport

import (
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/blobcatalog"

	"retrom/internal/persistence/recordstore"
)

func (run *creationRun) persistGroupValidation(record *groupRecord) error {
	if err := run.persistPreparedValidationArtifacts(record); err != nil {
		return err
	}
	if err := run.prepareRPGDependencies(record); err != nil {
		return err
	}
	status := record.group.ValidationStatus
	code := record.group.CompatibilityCode
	snapshot := record.group.DependencySnapshot
	if status == "" {
		status, code, snapshot = "READY", "READY", "{}"
	}
	var err error
	status, code, snapshot, err = resolveInitialArcadeBIOSState(
		run.ctx, run.transaction, run.plan.target.PlatformID,
		run.plan.target.ProviderID, run.plan.target.TargetID,
		record.group, status, code, snapshot,
	)
	if err != nil {
		return err
	}
	record.validationStatus, record.compatibilityCode = status, code
	if err := run.insertCoreValidation(record, snapshot); err != nil {
		return err
	}
	if err := run.insertDOSEntries(record); err != nil {
		return err
	}
	if err := run.insertDOSBundle(record); err != nil {
		return err
	}
	return run.insertValidationFiles(record)
}

func (run *creationRun) insertCoreValidation(record *groupRecord, dependencySnapshot string) error {
	target := run.plan.target
	inputDigest := prepublishDigest(prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: record.sourceSnapshotID, SourceManifestDigest: record.manifestDigest,
		ContentKind:              record.contentKind,
		TargetPlatformInstanceID: run.plan.request.TargetPlatformInstanceID,
		ProviderID:               target.ProviderID, TargetID: target.TargetID,
		ContentPolicyDigest: target.Policy.DigestFor(record.contentKind),
		DATVersionID:        nullStringPointer(run.plan.datID),
		DefaultDOSEntry:     stringPointer(record.group.DefaultDOSEntry),
		DependencySnapshot:  json.RawMessage(dependencySnapshot),
		Status:              record.validationStatus, CompatibilityCode: record.compatibilityCode,
	})
	_, err := recordstore.CreateImportItemCoreValidations(run.ctx, run.transaction, `
INSERT INTO import_item_core_validations(
  id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,
  provider_id,target_id,dat_version_id,
  default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
  status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`, record.validationID, record.itemID, run.plan.request.TargetPlatformInstanceID,
		target.Version, target.CoreID, target.ProviderID, target.TargetID,
		nullable(run.plan.datID), nullableText(record.group.DefaultDOSEntry),
		record.manifestDigest, record.sourceSnapshotID, inputDigest, record.validationStatus,
		record.compatibilityCode, dependencySnapshot, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *creationRun) insertDOSEntries(record *groupRecord) error {
	for _, entry := range record.group.DOSEntries {
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_dos_entries(
  import_item_id,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe,created_at_ms
) VALUES(?,?,?,?,?,1,?,?)
`, record.itemID, entry.Path, entry.Path, entry.Kind, entry.Rank, entry.Safe, run.now)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	return nil
}

func (run *creationRun) insertDOSBundle(record *groupRecord) error {
	bundleBlobID := record.group.BundleBlobID
	if record.group.Bundle != nil {
		var err error
		bundleBlobID, err = blobcatalog.EnsureRecord(
			run.ctx, run.transaction, *record.group.Bundle, "application/zip", run.now,
		)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	if bundleBlobID == "" {
		return nil
	}
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_validation_files(
  import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms
) VALUES(?,'DOS_LAUNCH_BUNDLE','game.zip',?,0,?)
`, record.validationID, bundleBlobID, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *creationRun) insertValidationFiles(record *groupRecord) error {
	for _, file := range record.group.ValidationFiles {
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_validation_files(
  import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms
) VALUES(?,?,?,?,?,?)
`, record.validationID, file.Role, file.LogicalName, file.BlobID, file.SortOrder, run.now)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	return nil
}
