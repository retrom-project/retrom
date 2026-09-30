package libraryimport

import (
	"context"
	"fmt"
	"strings"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/profilemodel"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) NextEmulatorID(ctx context.Context) (int64, error) {
	var id int64
	if err := dbapi.QueryRowContext(ctx, records.transaction,
		`SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variants`).Scan(&id); err != nil {
		return 0, fmt.Errorf("read next emulator game number: %w", err)
	}
	return id, nil
}

func (records reviewApprovalRecords) CreateVariant(ctx context.Context, v libraryservice.ApprovalVariant) error {
	result, err := recordstore.CreateGameVariants(ctx, records.transaction, `
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,
 status,compatibility_code,dependency_snapshot_json,default_dos_entry,version,created_at_ms,
updated_at_ms
) VALUES(?,?,?,?,?,?,?,'READY',?,?,?,1,?,?)`,
		v.ID, v.GameID, v.CoreID, v.ProviderID, v.TargetID, v.DATID, v.EmulatorGameID,
		v.CompatibilityCode, v.DependencyJSON, v.DefaultDOS, v.NowMS, v.NowMS)
	return approvalMutation(result, err, "insert approved variant", true)
}

func (records reviewApprovalRecords) CopyRuntimeFiles(
	ctx context.Context, source libraryservice.ApprovalRuntimeCopy,
) error {
	for _, file := range source.Files {
		record := file.FileRecord
		parsed, err := filestore.ParseRecord(record)
		if err != nil {
			return fmt.Errorf("read publication runtime file: %w", err)
		}
		if strings.HasPrefix(parsed.Path, filestore.ItemDirectory(source.ItemID)+"/payload/") {
			record, err = filestore.PublishedRecord(record, source.ItemID, source.GameID)
			if err != nil {
				return fmt.Errorf("publish runtime file: %w", err)
			}
		}

		result, err := recordstore.CreateVariantFiles(ctx, records.transaction, `
INSERT INTO variant_files(game_variant_id,role,logical_name,file_record,sort_order) VALUES(?,?,?,?,?)`,
			source.VariantID, file.Role, file.LogicalName, record, file.SortOrder)
		if err := approvalMutation(result, err, "publish runtime file", true); err != nil {
			return err
		}
	}
	return nil
}

func (records reviewApprovalRecords) CreateDependency(
	ctx context.Context, dep libraryservice.ApprovalVariantDependency,
) error {
	result, err := records.transaction.ExecContext(
		ctx,
		`
INSERT INTO variant_dependencies(
 game_variant_id,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,
state,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)`,
		dep.VariantID,
		dep.Kind,
		dep.Machine+".zip",
		dep.DATID,
		dep.Machine,
		dep.RequiredEntriesJSON,
		dep.State,
		dep.NowMS,
	)
	return approvalMutation(result, err, "insert approved variant dependency", true)
}

func (records reviewApprovalRecords) CreateRPGVariant(
	ctx context.Context, profile libraryservice.ApprovalRPGVariant,
) error {
	encoded, err := profilemodel.Encode(
		profilemodel.Variant,
		profilemodel.RPGMakerProject,
		&profilemodel.RPGVariant{
			Generation:               profile.Generation,
			DependencySnapshotSHA256: profile.DependencyDigest,
		},
	)
	if err != nil {
		return fmt.Errorf("encode approved RPG variant profile: %w", err)
	}
	result, err := records.transaction.ExecContext(ctx, `
UPDATE game_variants SET runtime_profile_json=?
WHERE id=? AND core_id='rpgmaker' AND runtime_profile_json IS NULL`, encoded, profile.VariantID)
	return approvalMutation(result, err, "insert approved RPG variant", true)
}
