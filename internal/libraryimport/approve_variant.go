package libraryimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type publishedVariantDependency struct {
	Kind            string   `json:"kind"`
	Machine         string   `json:"machine"`
	State           string   `json:"state"`
	RequiredEntries []string `json:"requiredEntries"`
}

func (run *approvalRun) persistVariant() error {
	var emulatorGameID int64
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variant_revisions
`).Scan(&emulatorGameID); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	inputDigest, err := approvalValidationInputDigest(approvalValidationDigestInput{
		VariantID: run.variantID, ContentID: run.contentID, ContentKind: run.contentKind,
		ArtifactID: run.artifactID, ArtifactVersion: run.artifactVersion,
		ArtifactCompatibility: run.artifactCompatibility, DATID: run.datID,
		ValidationID: run.validationID, Snapshot: run.validationSnapshot, SnapshotValid: run.snapshotValid,
	})
	if err != nil {
		return err
	}
	defaultDOSEntry := run.validationDOSEntry
	if run.draftDOSEntry.Valid {
		defaultDOSEntry = run.draftDOSEntry
	}
	compatibilityCode := "READY"
	if run.screenshotOverride {
		compatibilityCode = reviewScreenshotOverrideCode
	}
	if err := run.insertVariantRows(
		emulatorGameID, inputDigest, compatibilityCode, defaultDOSEntry,
	); err != nil {
		return err
	}
	if err := run.insertVariantDependencies(); err != nil {
		return err
	}
	return run.copyDOSEntriesAndSelectVariant()
}

func (run *approvalRun) insertVariantRows(
	emulatorGameID int64,
	inputDigest string,
	compatibilityCode string,
	defaultDOSEntry sql.NullString,
) error {
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,NULL,1,?,?)
`, run.variantID, run.gameID, run.coreID, run.now, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO game_variant_revisions(
  id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,
  validation_input_digest,emulator_game_id,status,compatibility_code,
  dependency_snapshot_json,default_dos_entry,created_at_ms
) VALUES(?,?,?,?,?,?,?,'READY',?,?,?,?)
`, run.variantRevisionID, run.variantID, run.contentID, run.artifactID, nullable(run.datID),
		inputDigest, emulatorGameID, compatibilityCode, run.runtimeDependencySnapshotJSON,
		nullable(defaultDOSEntry), run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
SELECT ?,role,logical_name,blob_id,sort_order
FROM import_item_validation_files WHERE import_item_core_validation_id=?
`, run.variantRevisionID, run.validationID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) insertVariantDependencies() error {
	var snapshot struct {
		Dependencies []publishedVariantDependency `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(run.dependencySnapshotJSON), &snapshot); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	for _, dependency := range snapshot.Dependencies {
		if err := run.insertVariantDependency(dependency); err != nil {
			return err
		}
	}
	return nil
}

func (run *approvalRun) insertVariantDependency(dependency publishedVariantDependency) error {
	if !run.datID.Valid || (dependency.Kind != "PARENT" && dependency.Kind != "BIOS_OR_BASE") {
		return ErrInvalid
	}
	requiredEntries, _ := json.Marshal(dependency.RequiredEntries)
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO variant_dependencies(
  game_variant_revision_id,kind,logical_archive,dat_version_id,source_machine_name,
  required_entries_json,state,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)
`, run.variantRevisionID, dependency.Kind, dependency.Machine+".zip", run.datID.String,
		dependency.Machine, string(requiredEntries), dependency.State, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) copyDOSEntriesAndSelectVariant() error {
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO dos_entries(
  game_content_revision_id,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe
)
SELECT ?,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe
FROM import_item_dos_entries WHERE import_item_id=?
`, run.contentID, run.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
UPDATE game_variants SET current_revision_id=?,updated_at_ms=? WHERE id=?
`, run.variantRevisionID, run.now, run.variantID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}
