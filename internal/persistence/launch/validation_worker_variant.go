package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/launch"
)

func (records validationWorkerRecords) Apply(ctx context.Context, plan application.ValidationVariantWrite) error {
	inputs, outcome := plan.Inputs, plan.Outcome
	dos, emulatorID, err := records.validationDefaults(ctx, inputs.GameVariantID, inputs.GameID, outcome.Status)
	if err != nil {
		return err
	}
	result, err := recordstore.UpdateGameVariants(ctx, records.executor, recordstore.Update{
		Set: `provider_id=?,target_id=?,dat_version_id=?,emulator_game_id=?,status=?,compatibility_code=?,
 dependency_snapshot_json=?,default_dos_entry=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND game_id=?`, Args: []any{inputs.GameVariantID, inputs.GameID}},
		Values: []any{
			inputs.ProviderID,
			inputs.TargetID,
			inputs.DATVersionID,
			emulatorID,
			outcome.Status,
			outcome.Code,
			outcome.DependencyJSON,
			dos,
			plan.NowMS,
		},
	})
	if err := validationAffected(result, err); err != nil {
		return err
	}
	return records.replaceValidationBIOSFiles(ctx, inputs.GameVariantID, outcome.BIOS)
}

func (records validationWorkerRecords) validationDefaults(
	ctx context.Context,
	variantID, gameID, status string,
) (sql.NullString, any, error) {
	var defaultDOSEntry sql.NullString
	if err := records.executor.QueryRowContext(ctx, `
SELECT COALESCE(
 (SELECT default_dos_entry FROM game_variants WHERE id=? AND game_id=?),
 (SELECT original_relative_path FROM dos_entries
  WHERE game_id=? AND enabled=1 AND direct_launch_safe=1
  ORDER BY rank,normalized_path LIMIT 1))
`, variantID, gameID, gameID).Scan(&defaultDOSEntry); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, nil, fmt.Errorf("load validation DOS entry: %w", err)
	}
	if status != "READY" {
		return defaultDOSEntry, nil, nil
	}
	var existing sql.NullInt64
	if err := records.executor.QueryRowContext(ctx, `
SELECT emulator_game_id FROM game_variants WHERE id=?
`, variantID).Scan(&existing); err != nil {
		return sql.NullString{}, nil, fmt.Errorf("load emulator game ID: %w", err)
	}
	if existing.Valid {
		return defaultDOSEntry, existing.Int64, nil
	}
	var emulatorGameID int64
	if err := records.executor.QueryRowContext(ctx, `
SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variants
`).Scan(&emulatorGameID); err != nil {
		return sql.NullString{}, nil, fmt.Errorf("allocate emulator game ID: %w", err)
	}
	return defaultDOSEntry, emulatorGameID, nil
}

func (records validationWorkerRecords) replaceValidationBIOSFiles(
	ctx context.Context,
	variantID string,
	biosSnapshot corevalidation.Snapshot,
) error {
	if _, err := records.executor.ExecContext(ctx, `
DELETE FROM variant_files WHERE game_variant_id=? AND role='BIOS_BUNDLE'
`, variantID); err != nil {
		return fmt.Errorf("delete current validation BIOS files: %w", err)
	}
	for sortOrder, dependency := range biosSnapshot.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		if _, err := recordstore.CreateVariantFiles(ctx, records.executor, `
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
VALUES(?,'BIOS_BUNDLE',?,?,?)
`, variantID, dependency.LogicalName, *dependency.BlobID, sortOrder); err != nil {
			return fmt.Errorf("insert current validation BIOS file: %w", err)
		}
	}
	return nil
}
