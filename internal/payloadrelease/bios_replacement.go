package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type BIOSReplacementImpact struct {
	BlobIDs        []string
	SaveStateCount int64
}

// RetireSupersededBIOS makes BIOS replacement a destructive dependency
// boundary while retaining installation and variant rows as audit evidence.
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
UPDATE bios_installations SET is_active=0,blob_id=NULL,payload_released_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND is_active=1
`, now, now, installationID); err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/release BIOS installation: %w", err)
	}
	if err := execBatches(ctx, transaction, `
DELETE FROM variant_files WHERE rowid IN (
 SELECT file.rowid FROM variant_files file
 JOIN game_variant_revisions revision ON revision.id=file.game_variant_revision_id
 WHERE file.role='BIOS_BUNDLE' AND file.blob_id=? AND EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 ) ORDER BY file.rowid LIMIT 200
)
`, blobID, installationID); err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/delete BIOS variant payload: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
DELETE FROM save_states WHERE game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision
 WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)
`, installationID)
	if err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/delete BIOS saves: %w", err)
	}
	saveCount, err := result.RowsAffected()
	if err != nil {
		return BIOSReplacementImpact{}, fmt.Errorf("payloadrelease/count BIOS saves: %w", err)
	}
	for _, statement := range affectedBIOSDeleteStatements() {
		if err := execBatches(ctx, transaction, statement, installationID); err != nil {
			return BIOSReplacementImpact{}, err
		}
	}
	if err := scheduleBIOSConsumption(ctx, transaction, installationID, now); err != nil {
		return BIOSReplacementImpact{}, err
	}
	return BIOSReplacementImpact{BlobIDs: blobIDs, SaveStateCount: saveCount}, nil
}

func affectedBIOSBlobIDs(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
) ([]string, error) {
	return collectIDs(ctx, transaction, `
SELECT save.state_blob_id FROM save_states save
JOIN game_variant_revisions revision ON revision.id=save.game_variant_revision_id
WHERE EXISTS(
 SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?
)
UNION ALL
SELECT save.screenshot_blob_id FROM save_states save
JOIN game_variant_revisions revision ON revision.id=save.game_variant_revision_id
WHERE EXISTS(
 SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?
)
UNION ALL
SELECT file.blob_id FROM launch_content_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
WHERE EXISTS(
 SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?
)
UNION ALL
SELECT file.blob_id FROM launch_external_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
WHERE EXISTS(
 SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?
)
`, installationID, installationID, installationID, installationID)
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
updated_at_ms=?,version=version+1 WHERE state NOT IN ('FINISHED','FAILED') AND game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{now, now, installationID}},
		{`UPDATE netplay_rooms SET state='ENDED',current_session_id=NULL,ended_at_ms=?,
end_reason='BIOS_REPLACED',updated_at_ms=?,version=version+1
WHERE state IN ('WAITING','STARTING','RUNNING') AND selected_game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{now, now, installationID}},
		{`UPDATE launch_sessions SET save_state_id=NULL WHERE save_state_id IN (
 SELECT save.id FROM save_states save JOIN game_variant_revisions revision
 ON revision.id=save.game_variant_revision_id WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{installationID}},
		{`UPDATE launch_sessions SET state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),
updated_at_ms=?,version=version+1 WHERE state IN ('CREATED','ACTIVE') AND game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{now, now, installationID}},
		{`UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE state='ACTIVE' AND game_variant_revision_id IN (
 SELECT revision.id FROM game_variant_revisions revision WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 )
)`, []any{now, now, installationID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("payloadrelease/stop BIOS runtime: %w", err)
		}
	}
	return nil
}

func affectedBIOSDeleteStatements() []string {
	return []string{
		`DELETE FROM launch_external_files WHERE rowid IN (
 SELECT file.rowid FROM launch_external_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
 WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 ) ORDER BY file.rowid LIMIT 200
)`,
		`DELETE FROM launch_content_files WHERE rowid IN (
 SELECT file.rowid FROM launch_content_files file
 JOIN launch_sessions launch ON launch.id=file.launch_session_id
 JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
 WHERE EXISTS(
  SELECT 1 FROM json_each(revision.dependency_snapshot_json,'$.bios') dependency
  WHERE json_extract(dependency.value,'$.installationId')=?
 ) ORDER BY file.rowid LIMIT 200
)`,
	}
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
