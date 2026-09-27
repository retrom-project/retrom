package payloadlaunch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/retirementops"
	"retrom/internal/persistence/sessionstore"
	application "retrom/internal/service/cleanupjobs"
)

type Records struct{ Executor dbapi.Executor }

func (records Records) Launch(
	ctx context.Context, now int64, limit int,
) (application.LaunchRetirement, error) {
	var facts application.LaunchRetirement
	var idle, finished sql.NullInt64
	err := dbapi.QueryRowContext(ctx, records.Executor, `SELECT launch.id,launch.state,launch.version,retirement.due_at_ms,
launch.bootstrap_expires_at_ms,launch.hard_expires_at_ms,launch.idle_expires_at_ms,launch.finished_at_ms
FROM launch_payload_retirements retirement JOIN launch_sessions launch ON launch.id=retirement.launch_session_id
WHERE retirement.released_at_ms IS NULL AND retirement.due_at_ms<=?
ORDER BY retirement.due_at_ms,retirement.launch_session_id LIMIT 1`, now).
		Scan(&facts.ID, &facts.State, &facts.Version, &facts.DueMS, &facts.BootstrapMS, &facts.HardMS, &idle, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return facts, fmt.Errorf("read launch retirement: %w", err)
	}
	facts.Found = true
	facts.Idle = application.WorkTime{Set: idle.Valid, Value: idle.Int64}
	facts.Finished = application.WorkTime{Set: finished.Valid, Value: finished.Int64}
	facts.Content, err = retirementops.Files(ctx, records.Executor,
		`SELECT launch_session_id,logical_name,blob_id FROM launch_content_files
WHERE launch_session_id=? ORDER BY logical_name LIMIT ?`, facts.ID, limit)
	if err != nil {
		return application.LaunchRetirement{}, wrapErr(err)
	}
	facts.External, err = retirementops.Files(ctx, records.Executor,
		`SELECT launch_session_id,virtual_path,blob_id FROM launch_external_files
WHERE launch_session_id=? ORDER BY virtual_path LIMIT ?`, facts.ID, limit)
	if err != nil {
		return application.LaunchRetirement{}, wrapErr(err)
	}
	facts.Plays, err = retirementops.Plays(ctx, records.Executor, facts.ID)
	if err != nil {
		return application.LaunchRetirement{}, wrapErr(err)
	}
	return facts, nil
}

func (records Records) FenceLaunch(ctx context.Context, before application.LaunchRetirement) error {
	idle, finished := retirementops.Time(before.Idle), retirementops.Time(before.Finished)
	result, err := records.Executor.ExecContext(ctx, `UPDATE launch_sessions SET version=version
WHERE id=? AND state=? AND version=? AND bootstrap_expires_at_ms=? AND hard_expires_at_ms=?
AND (idle_expires_at_ms=? OR (idle_expires_at_ms IS NULL AND ? IS NULL))
AND (finished_at_ms=? OR (finished_at_ms IS NULL AND ? IS NULL))
AND EXISTS(SELECT 1 FROM launch_payload_retirements WHERE launch_session_id=?
AND due_at_ms=? AND released_at_ms IS NULL)`,
		before.ID, before.State, before.Version, before.BootstrapMS, before.HardMS,
		idle, idle, finished, finished, before.ID, before.DueMS)
	if err := retirementops.Write(result, err, 1); err != nil {
		return fmt.Errorf("fence launch retirement: %w", err)
	}
	return nil
}

func (records Records) TerminateLaunch(ctx context.Context, change application.LaunchRetirementEnd) error {
	before := change.Before
	if change.Expire {
		result, err := sessionstore.ChangeLaunch(ctx, records.Executor, recordstore.Update{
			Set: `state=?,finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1`,
			Scope: recordstore.Scope{
				Where: `id=? AND state=? AND version=?`, Args: []any{before.ID, before.State, before.Version},
			},
			Values: []any{change.State, change.NowMS, change.NowMS},
		})
		if err := retirementops.Write(result, err, 1); err != nil {
			return fmt.Errorf("expire retired launch: %w", err)
		}
	}
	for _, play := range before.Plays {
		result, err := records.Executor.ExecContext(ctx, `UPDATE play_sessions SET state=?,ended_at_ms=?,
updated_at_ms=?,version=version+1 WHERE id=? AND version=? AND state='ACTIVE' AND launch_session_id=?`,
			change.PlayState, change.NowMS, change.NowMS, play.ID, play.Version, before.ID)
		if err := retirementops.Write(result, err, 1); err != nil {
			return fmt.Errorf("end retired play: %w", err)
		}
	}
	return nil
}

func (records Records) ReleaseLaunchFiles(ctx context.Context, before application.LaunchRetirement) error {
	if err := retirementops.DeleteFiles(ctx, records.Executor, recordstore.DeleteLaunchExternalFiles,
		`(launch_session_id,virtual_path,blob_id)`, before.External); err != nil {
		return wrapErr(err)
	}
	return wrapErr(retirementops.DeleteFiles(ctx, records.Executor, recordstore.DeleteLaunchContentFiles,
		`(launch_session_id,logical_name,blob_id)`, before.Content))
}

func (records Records) CompleteLaunch(ctx context.Context, change application.RetirementCompletion) error {
	result, err := records.Executor.ExecContext(ctx, `UPDATE launch_payload_retirements SET released_at_ms=?
WHERE launch_session_id=? AND due_at_ms=? AND released_at_ms IS NULL
AND NOT EXISTS(SELECT 1 FROM launch_content_files WHERE launch_session_id=?)
AND NOT EXISTS(SELECT 1 FROM launch_external_files WHERE launch_session_id=?)`,
		change.NowMS, change.ID, change.DueMS, change.ID, change.ID)
	if err := retirementops.Write(result, err, 1); err != nil {
		return fmt.Errorf("complete launch retirement: %w", err)
	}
	return nil
}
