package firmware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	cleanupjobs "retrom/internal/persistence/uploads/payloadpurge"
	"retrom/internal/service/firmware"
)

type supersessionRecords struct{ executor dbapi.Executor }

func BindSupersession(executor dbapi.Executor) firmware.SupersessionScope {
	records := supersessionRecords{executor: executor}
	return firmware.SupersessionScope{Read: records, Write: records, Payload: cleanupjobs.BindScheduling(executor)}
}

func (records supersessionRecords) Current(
	ctx context.Context, requirementID string,
) (firmware.SupersededInstallation, bool, error) {
	var result firmware.SupersededInstallation
	err := dbapi.QueryRowContext(ctx, records.executor, `SELECT id,requirement_id,file_record,version
FROM bios_installations WHERE requirement_id=? AND is_active=1`, requirementID).
		Scan(&result.ID, &result.RequirementID, &result.FileRecord, &result.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return firmware.SupersededInstallation{}, false, nil
	}
	if err != nil {
		return firmware.SupersededInstallation{}, false, fmt.Errorf("read current BIOS installation: %w", err)
	}
	return result, true, nil
}

func (records supersessionRecords) Consumption(ctx context.Context, installationID string) (string, error) {
	var id string
	err := dbapi.QueryRowContext(ctx, records.executor, `SELECT id FROM upload_consumptions
WHERE consumer_type='BIOS_INSTALLATION' AND consumer_id=? AND released_at_ms IS NULL`, installationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read BIOS upload consumption: %w", err)
	}
	return id, nil
}

func (records supersessionRecords) Deactivate(
	ctx context.Context, before firmware.SupersededInstallation, now int64,
) error {
	if err := changed(recordstore.UpdateBiosInstallations(ctx, records.executor, recordstore.Update{
		Set: `is_active=0,version=version+1,updated_at_ms=?`, Values: []any{now},
		Scope: recordstore.Scope{
			Where: `id=? AND requirement_id=? AND file_record=? AND version=? AND is_active=1`,
			Args:  []any{before.ID, before.RequirementID, before.FileRecord, before.Version},
		},
	})); err != nil {
		return err
	}
	return records.revokeBIOSLaunches(ctx, before.ID, before.FileRecord, now)
}

func (records supersessionRecords) revokeBIOSLaunches(
	ctx context.Context, installationID, fileRecord string, now int64,
) error {
	rows, err := records.executor.QueryContext(ctx, `
SELECT DISTINCT launch.id FROM launch_sessions launch
JOIN launch_external_files file ON file.launch_session_id=launch.id
WHERE file.file_record=? AND file.kind IN ('BIOS','BIOS_BUNDLE')
AND EXISTS(SELECT 1 FROM json_each(launch.dependency_snapshot_json,'$.bios') dependency
 WHERE json_extract(dependency.value,'$.installationId')=?)`, fileRecord, installationID)
	if err != nil {
		return fmt.Errorf("find launches using superseded BIOS: %w", err)
	}
	defer func() { cleanup.Error("close launches using superseded BIOS", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("read launch using superseded BIOS: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate launches using superseded BIOS: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close launches using superseded BIOS: %w", err)
	}
	for _, id := range ids {
		if _, err := sessionstore.ChangeLaunch(ctx, records.executor, recordstore.Update{
			Set:    `state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1`,
			Values: []any{now, now},
			Scope:  recordstore.Scope{Where: `id=? AND state IN ('CREATED','ACTIVE')`, Args: []any{id}},
		}); err != nil {
			return fmt.Errorf("revoke launch using superseded BIOS: %w", err)
		}
		if _, err := records.executor.ExecContext(ctx, `
UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE launch_session_id=? AND state='ACTIVE'`, now, now, id); err != nil {
			return fmt.Errorf("abandon play using superseded BIOS: %w", err)
		}
		if _, err := recordstore.DeleteLaunchExternalFiles(ctx, records.executor, recordstore.Scope{
			Where: `launch_session_id=?`, Args: []any{id},
		}); err != nil {
			return fmt.Errorf("remove superseded launch externals: %w", err)
		}
		if _, err := recordstore.DeleteLaunchContentFiles(ctx, records.executor, recordstore.Scope{
			Where: `launch_session_id=?`, Args: []any{id},
		}); err != nil {
			return fmt.Errorf("remove superseded launch content: %w", err)
		}
	}
	return nil
}
