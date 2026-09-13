package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	"retrom/internal/repo/sessionstore"
	application "retrom/internal/service/payloadrelease"
)

func (records retirementRecords) FenceLaunch(ctx context.Context, before application.LaunchRetirement) error {
	idle, finished := retirementTime(before.Idle), retirementTime(before.Finished)
	result, err := records.executor.ExecContext(ctx, `UPDATE launch_sessions SET version=version
WHERE id=? AND state=? AND version=? AND bootstrap_expires_at_ms=? AND hard_expires_at_ms=?
AND (idle_expires_at_ms=? OR (idle_expires_at_ms IS NULL AND ? IS NULL))
AND (finished_at_ms=? OR (finished_at_ms IS NULL AND ? IS NULL))
AND EXISTS(SELECT 1 FROM launch_payload_retirements WHERE launch_session_id=?
AND due_at_ms=? AND released_at_ms IS NULL)`,
		before.ID, before.State, before.Version, before.BootstrapMS, before.HardMS,
		idle, idle, finished, finished, before.ID, before.DueMS)
	if err := retirementWrite(result, err, 1); err != nil {
		return fmt.Errorf("fence launch retirement: %w", err)
	}
	return nil
}

func (records retirementRecords) TerminateLaunch(ctx context.Context, change application.LaunchRetirementEnd) error {
	before := change.Before
	if change.Expire {
		result, err := sessionstore.ChangeLaunch(ctx, records.executor, recordstore.Update{
			Set: `state=?,finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1`,
			Scope: recordstore.Scope{
				Where: `id=? AND state=? AND version=?`, Args: []any{before.ID, before.State, before.Version},
			},
			Values: []any{change.State, change.NowMS, change.NowMS},
		})
		if err := retirementWrite(result, err, 1); err != nil {
			return fmt.Errorf("expire retired launch: %w", err)
		}
	}
	for _, play := range before.Plays {
		result, err := records.executor.ExecContext(ctx, `UPDATE play_sessions SET state=?,ended_at_ms=?,
updated_at_ms=?,version=version+1 WHERE id=? AND version=? AND state='ACTIVE' AND launch_session_id=?`,
			change.PlayState, change.NowMS, change.NowMS, play.ID, play.Version, before.ID)
		if err := retirementWrite(result, err, 1); err != nil {
			return fmt.Errorf("end retired play: %w", err)
		}
	}
	return nil
}

func (records retirementRecords) ReleaseLaunchFiles(ctx context.Context, before application.LaunchRetirement) error {
	if err := records.deleteFiles(ctx, recordstore.DeleteLaunchExternalFiles,
		`(launch_session_id,virtual_path,blob_id)`, before.External); err != nil {
		return err
	}
	return records.deleteFiles(ctx, recordstore.DeleteLaunchContentFiles,
		`(launch_session_id,logical_name,blob_id)`, before.Content)
}

func (records retirementRecords) CompleteLaunch(ctx context.Context, change application.RetirementCompletion) error {
	result, err := records.executor.ExecContext(ctx, `UPDATE launch_payload_retirements SET released_at_ms=?
WHERE launch_session_id=? AND due_at_ms=? AND released_at_ms IS NULL
AND NOT EXISTS(SELECT 1 FROM launch_content_files WHERE launch_session_id=?)
AND NOT EXISTS(SELECT 1 FROM launch_external_files WHERE launch_session_id=?)`,
		change.NowMS, change.ID, change.DueMS, change.ID, change.ID)
	if err := retirementWrite(result, err, 1); err != nil {
		return fmt.Errorf("complete launch retirement: %w", err)
	}
	return nil
}

type retirementDelete func(context.Context, dbexec.Executor, recordstore.Scope) (sql.Result, error)

func (records retirementRecords) deleteFiles(ctx context.Context, remove retirementDelete,
	columns string, files []application.RetirementFile,
) error {
	if len(files) == 0 {
		return nil
	}
	args := make([]any, 0, len(files)*3)
	for _, file := range files {
		args = append(args, file.OwnerID, file.Name, file.BlobID)
	}
	where := columns + ` IN (` + strings.TrimSuffix(strings.Repeat("(?,?,?),", len(files)), ",") + `)`
	result, err := remove(ctx, records.executor, recordstore.Scope{Where: where, Args: args})
	if err := retirementWrite(result, err, int64(len(files))); err != nil {
		return fmt.Errorf("remove retired files: %w", err)
	}
	return nil
}

func retirementTime(value application.WorkTime) any {
	if !value.Set {
		return nil
	}
	return value.Value
}
