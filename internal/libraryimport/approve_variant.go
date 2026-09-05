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
	var emulatorGameID any
	if run.providerID == "emulatorjs" {
		var nextID int64
		if err := run.transaction.QueryRowContext(run.ctx, `
SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variants
		`).Scan(&nextID); err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		emulatorGameID = nextID
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
		emulatorGameID, compatibilityCode, defaultDOSEntry,
	); err != nil {
		return err
	}
	if err := run.insertVariantDependencies(); err != nil {
		return err
	}
	if err := run.insertRPGMakerVariantProfile(); err != nil {
		return err
	}
	if err := run.copyRPGMakerRuntimePacks(); err != nil {
		return err
	}
	return run.copyDOSEntriesAndSelectVariant()
}

func (run *approvalRun) insertVariantRows(
	emulatorGameID any,
	compatibilityCode string,
	defaultDOSEntry sql.NullString,
) error {
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO game_variants(
  id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,
  status,compatibility_code,dependency_snapshot_json,default_dos_entry,
  version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,'READY',?,?,?,1,?,?)
`, run.variantID, run.gameID, run.coreID, run.providerID, run.targetID,
		nullable(run.datID), emulatorGameID, compatibilityCode, run.runtimeDependencySnapshotJSON,
		nullable(defaultDOSEntry), run.now, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
SELECT ?,role,logical_name,blob_id,sort_order
FROM import_item_validation_files WHERE import_item_core_validation_id=?
`, run.variantID, run.validationID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) insertRPGMakerVariantProfile() error {
	if run.platformID != "rpgmaker" {
		return nil
	}
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO rpgmaker_variant_profiles(
  game_variant_id,generation,dependency_snapshot_sha256
) VALUES(?,?,?)
`, run.variantID, run.rpgGeneration, run.rpgDependencySnapshotSHA)
	if err != nil {
		return fmt.Errorf("libraryimport/rpgmaker variant profile: %w", err)
	}
	return nil
}

func (run *approvalRun) copyRPGMakerRuntimePacks() error {
	if run.platformID != "rpgmaker" {
		return nil
	}
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO game_variant_runtime_packs(
  game_variant_id,slot,declared_name,normalized_declared_name,definition_id,installation_id
)
SELECT ?,slot,declared_name,normalized_declared_name,definition_id,installation_id
FROM review_draft_runtime_pack_selections
WHERE review_draft_id=?
ORDER BY slot
`, run.variantID, run.draftID)
	if err != nil {
		return fmt.Errorf("libraryimport/rpgmaker variant packs: %w", err)
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
  game_variant_id,kind,logical_archive,dat_version_id,source_machine_name,
  required_entries_json,state,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)
`, run.variantID, dependency.Kind, dependency.Machine+".zip", run.datID.String,
		dependency.Machine, string(requiredEntries), dependency.State, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) copyDOSEntriesAndSelectVariant() error {
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO dos_entries(
  game_id,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe
)
SELECT ?,normalized_path,original_relative_path,kind,rank,enabled,direct_launch_safe
FROM import_item_dos_entries WHERE import_item_id=?
`, run.gameID, run.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}
