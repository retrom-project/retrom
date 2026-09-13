package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) NextEmulatorID(ctx context.Context) (int64, error) {
	var id int64
	if err := records.transaction.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variants`).Scan(&id); err != nil {
		return 0, fmt.Errorf("read next emulator game number: %w", err)
	}
	return id, nil
}

func (records reviewApprovalRecords) CreateVariant(ctx context.Context, v application.ApprovalVariant) error {
	result, err := recordstore.CreateGameVariants(ctx, records.transaction, `
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,
 status,compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,'READY',?,?,?,1,?,?)`,
		v.ID, v.GameID, v.CoreID, v.ProviderID, v.TargetID, v.DATID, v.EmulatorGameID,
		v.CompatibilityCode, v.DependencyJSON, v.DefaultDOS, v.NowMS, v.NowMS)
	return approvalMutation(result, err, "insert approved variant", true)
}

func (records reviewApprovalRecords) CopyValidationFiles(
	ctx context.Context, source application.ApprovalValidationCopy,
) error {
	result, err := recordstore.CreateVariantFiles(ctx, records.transaction, `
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
SELECT ?,role,logical_name,blob_id,sort_order
FROM import_item_validation_files WHERE import_item_core_validation_id=?`, source.VariantID, source.ValidationID)
	return approvalMutation(result, err, "copy approved validation files", false)
}

func (records reviewApprovalRecords) CreateDependency(
	ctx context.Context, dep application.ApprovalVariantDependency,
) error {
	result, err := records.transaction.ExecContext(ctx, `
INSERT INTO variant_dependencies(
 game_variant_id,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,state,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)`,
		dep.VariantID, dep.Kind, dep.Machine+".zip", dep.DATID, dep.Machine, dep.RequiredEntriesJSON, dep.State, dep.NowMS)
	return approvalMutation(result, err, "insert approved variant dependency", true)
}

func (records reviewApprovalRecords) CreateRPGVariant(
	ctx context.Context, profile application.ApprovalRPGVariant,
) error {
	result, err := records.transaction.ExecContext(ctx, `
INSERT INTO rpgmaker_variant_profiles(game_variant_id,generation,dependency_snapshot_sha256) VALUES(?,?,?)`,
		profile.VariantID, profile.Generation, profile.DependencyDigest)
	return approvalMutation(result, err, "insert approved RPG variant", true)
}
