package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"
)

type Retirement struct{ database *sql.DB }

func NewRetirement(database *sql.DB) *Retirement { return &Retirement{database: database} }

func (repository *Retirement) WithRetirement(ctx context.Context, run func(application.RetirementScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retirement transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := retirementRecords{executor: tx}
	if err := run(application.RetirementScope{Read: records, BIOS: records, Launch: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retirement transaction: %w", err)
	}
	return nil
}

type retirementRecords struct{ executor dbexec.Executor }

func (records retirementRecords) BIOS(ctx context.Context, limit int) (application.BIOSRetirement, error) {
	var facts application.BIOSRetirement
	err := records.executor.QueryRowContext(ctx, `SELECT id,blob_id,version,
EXISTS(SELECT 1 FROM bios_installations active WHERE active.blob_id=retired.blob_id AND active.is_active=1)
FROM bios_installations retired WHERE is_active=0 AND blob_id IS NOT NULL ORDER BY updated_at_ms,id LIMIT 1`).
		Scan(&facts.ID, &facts.BlobID, &facts.Version, &facts.SharedActive)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return facts, fmt.Errorf("read retired installation: %w", err)
	}
	facts.Found = true
	if !facts.SharedActive {
		facts.Files, err = records.files(ctx, `SELECT game_variant_id,logical_name,blob_id FROM variant_files
WHERE role='BIOS_BUNDLE' AND blob_id=? ORDER BY game_variant_id,logical_name LIMIT ?`, facts.BlobID, limit)
		if err != nil {
			return application.BIOSRetirement{}, err
		}
	}
	return facts, nil
}

func (records retirementRecords) Launch(
	ctx context.Context, now int64, limit int,
) (application.LaunchRetirement, error) {
	var facts application.LaunchRetirement
	var idle, finished sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `SELECT launch.id,launch.state,launch.version,retirement.due_at_ms,
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
	facts.Content, err = records.files(ctx, `SELECT launch_session_id,logical_name,blob_id FROM launch_content_files
WHERE launch_session_id=? ORDER BY logical_name LIMIT ?`, facts.ID, limit)
	if err != nil {
		return application.LaunchRetirement{}, err
	}
	facts.External, err = records.files(ctx, `SELECT launch_session_id,virtual_path,blob_id FROM launch_external_files
WHERE launch_session_id=? ORDER BY virtual_path LIMIT ?`, facts.ID, limit)
	if err != nil {
		return application.LaunchRetirement{}, err
	}
	facts.Plays, err = records.plays(ctx, facts.ID)
	if err != nil {
		return application.LaunchRetirement{}, err
	}
	return facts, nil
}

func (records retirementRecords) files(
	ctx context.Context, query string, args ...any,
) ([]application.RetirementFile, error) {
	rows, err := records.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select retirement files: %w", err)
	}
	defer func() { cleanup.Error("close retirement files", rows.Close()) }()
	var files []application.RetirementFile
	for rows.Next() {
		var file application.RetirementFile
		if err := rows.Scan(&file.OwnerID, &file.Name, &file.BlobID); err != nil {
			return nil, fmt.Errorf("scan retirement file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retirement files: %w", err)
	}
	return files, nil
}

func (records retirementRecords) plays(ctx context.Context, id string) ([]application.RetirementPlay, error) {
	var play application.RetirementPlay
	err := records.executor.QueryRowContext(ctx, `SELECT id,version FROM play_sessions
WHERE launch_session_id=? AND state='ACTIVE'`, id).Scan(&play.ID, &play.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read retiring play: %w", err)
	}
	return []application.RetirementPlay{play}, nil
}

func retirementWrite(result sql.Result, err error, expected int64) error {
	if err != nil {
		return fmt.Errorf("write retirement record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count retirement records: %w", err)
	}
	if count != expected {
		return application.ErrRetirementSnapshotChanged
	}
	return nil
}
