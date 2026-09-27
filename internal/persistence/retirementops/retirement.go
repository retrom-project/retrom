package retirementops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
	"strings"
)

func Files(executor dbapi.Executor,
	ctx context.Context, query string, args ...any,
) ([]application.RetirementFile, error) {
	rows, err := executor.QueryContext(ctx, query, args...)
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

func Plays(executor dbapi.Executor, ctx context.Context, id string) ([]application.RetirementPlay, error) {
	var play application.RetirementPlay
	err := dbapi.QueryRowContext(ctx, executor, `SELECT id,version FROM play_sessions
WHERE launch_session_id=? AND state='ACTIVE'`, id).Scan(&play.ID, &play.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read retiring play: %w", err)
	}
	return []application.RetirementPlay{play}, nil
}

func Write(result sql.Result, err error, expected int64) error {
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

type Delete func(context.Context, dbapi.Executor, recordstore.Scope) (sql.Result, error)

func DeleteFiles(ctx context.Context, executor dbapi.Executor, remove Delete,
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
	result, err := remove(ctx, executor, recordstore.Scope{Where: where, Args: args})
	if err := Write(result, err, int64(len(files))); err != nil {
		return fmt.Errorf("remove retired files: %w", err)
	}
	return nil
}

func Time(value application.WorkTime) any {
	if !value.Set {
		return nil
	}
	return value.Value
}
