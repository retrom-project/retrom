package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type BIOSReplacementImpact struct {
	BlobIDs []string
}

// RetireSupersededBIOS invalidates only current variants that used the old
// installation. Saves remain game-scoped and are restored by any later target
// that declares their checkpoint format readable.
func RetireSupersededBIOS(
	ctx context.Context,
	transaction *sql.Tx,
	requirementID string,
	now int64,
) (BIOSReplacementImpact, error) {
	var installationID, blobID string
	err := transaction.QueryRowContext(ctx, `
SELECT id,blob_id FROM bios_installations
WHERE requirement_id=? AND is_active=1
`, requirementID).Scan(&installationID, &blobID)
	if errors.Is(err, sql.ErrNoRows) {
		return BIOSReplacementImpact{}, nil
	}
	if err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/read active BIOS: %w", err)
	}
	blobIDs, err := affectedBIOSBlobIDs(ctx, transaction, installationID)
	if err != nil {
		return BIOSReplacementImpact{}, err
	}
	blobIDs = append(blobIDs, blobID)
	if err := stopAffectedBIOSRuntime(ctx, transaction, installationID, now); err != nil {
		return BIOSReplacementImpact{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM variant_files
WHERE role='BIOS_BUNDLE' AND blob_id=? AND game_variant_id IN (
 SELECT variant.id FROM game_variants variant
 WHERE EXISTS(SELECT 1 FROM json_each(variant.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?)
)
`, blobID, installationID); err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/delete current BIOS files: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET status='BLOCKED',compatibility_code='LAUNCH_BIOS_MISSING',emulator_game_id=NULL,
version=version+1,updated_at_ms=?
WHERE EXISTS(SELECT 1 FROM json_each(game_variants.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?)
`, now, installationID); err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/invalidate current variants: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE bios_installations SET is_active=0,blob_id=NULL,payload_released_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND is_active=1
`, now, now, installationID); err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/release BIOS installation: %w", err)
	}
	if err := scheduleBIOSConsumption(ctx, transaction, installationID, now); err != nil {
		return BIOSReplacementImpact{}, err
	}
	return BIOSReplacementImpact{BlobIDs: uniqueStrings(blobIDs)}, nil
}

func affectedBIOSBlobIDs(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT file.blob_id FROM variant_files file
JOIN game_variants variant ON variant.id=file.game_variant_id
WHERE EXISTS(SELECT 1 FROM json_each(variant.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?)
UNION ALL
SELECT file.blob_id FROM launch_content_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id
WHERE EXISTS(SELECT 1 FROM json_each(launch.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?)
UNION ALL
SELECT file.blob_id FROM launch_external_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id
WHERE EXISTS(SELECT 1 FROM json_each(launch.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?)
`, installationID, installationID, installationID)
}

func stopAffectedBIOSRuntime(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
	now int64,
) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE netplay_sessions SET state='FAILED',finished_at_ms=?,end_reason='BIOS_REPLACED',
updated_at_ms=?,version=version+1 WHERE state NOT IN ('FINISHED','FAILED') AND game_variant_id IN (
 SELECT variant.id FROM game_variants variant WHERE EXISTS(
  SELECT 1 FROM json_each(variant.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{now, now, installationID}},
		{`UPDATE netplay_rooms SET state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason='BIOS_REPLACED',
updated_at_ms=?,version=version+1 WHERE state IN ('WAITING','STARTING','RUNNING') AND selected_game_variant_id IN (
 SELECT variant.id FROM game_variants variant WHERE EXISTS(
  SELECT 1 FROM json_each(variant.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{now, now, installationID}},
		{`UPDATE launch_sessions SET state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),
updated_at_ms=?,version=version+1 WHERE state IN ('CREATED','ACTIVE') AND EXISTS(
 SELECT 1 FROM json_each(launch_sessions.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?
)`, []any{now, now, installationID}},
		{`UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE state='ACTIVE' AND game_id IN (
 SELECT variant.game_id FROM game_variants variant WHERE EXISTS(
  SELECT 1 FROM json_each(variant.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{now, now, installationID}},
		{`DELETE FROM launch_external_files WHERE launch_session_id IN (
 SELECT launch.id FROM launch_sessions launch
 WHERE EXISTS(SELECT 1 FROM json_each(launch.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?)
)`, []any{installationID}},
		{`DELETE FROM launch_content_files WHERE launch_session_id IN (
 SELECT launch.id FROM launch_sessions launch
 WHERE EXISTS(SELECT 1 FROM json_each(launch.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?)
)`, []any{installationID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("payloadrelease/stop BIOS runtime: %w", err)
		}
	}
	return nil
}

func scheduleBIOSConsumption(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
	now int64,
) error {
	var consumptionID string
	err := transaction.QueryRowContext(ctx, `
SELECT id FROM upload_consumptions
WHERE consumer_type='BIOS_INSTALLATION' AND consumer_id=? AND released_at_ms IS NULL
`, installationID).Scan(&consumptionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("payloadrelease/read BIOS consumption: %w", err)
	}
	if _, err := ScheduleConsumption(ctx, transaction, consumptionID, now); err != nil {
		return fmt.Errorf("payloadrelease/schedule BIOS consumption: %w", err)
	}
	return nil
}
