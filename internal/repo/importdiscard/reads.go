package importdiscard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/service/importdiscard"
)

func (records records) Batch(ctx context.Context, key importdiscard.Key) (importdiscard.Batch, error) {
	table, err := batchTable(key.Kind)
	if err != nil {
		return importdiscard.Batch{}, err
	}
	fields := "import_job_id IS NOT NULL,0,0,''"
	if key.Kind == "IMPORT" {
		fields = "1,rejected_file_count,resolved_rejected_file_count,payload_state"
	}
	var batch importdiscard.Batch
	err = records.executor.QueryRowContext(ctx, `SELECT state,version,`+fields+` FROM `+table+` WHERE id=?`, key.ID).
		Scan(&batch.State, &batch.Version, &batch.Started, &batch.Rejected, &batch.ResolvedRejected, &batch.PayloadState)
	if errors.Is(err, sql.ErrNoRows) {
		return batch, importdiscard.ErrNotFound
	}
	if err != nil {
		return batch, fmt.Errorf("read discard batch: %w", err)
	}
	batch.ItemCounts, err = records.itemCounts(ctx, key, table)
	return batch, err
}

func (records records) itemCounts(ctx context.Context, key importdiscard.Key, table string) (map[string]int64, error) {
	items, state, foreignKey := table[:len(table)-1]+"_items", "execution_state", "import_id"
	if key.Kind == "IMPORT" {
		items, state, foreignKey = "import_items", "state", "import_job_id"
	}
	rows, err := records.executor.QueryContext(
		ctx,
		`SELECT `+state+`,count(*) FROM `+items+` WHERE `+foreignKey+`=? GROUP BY `+state,
		key.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("read discard item states: %w", err)
	}
	defer func() { cleanup.Error("close discard item counts", rows.Close()) }()
	result := make(map[string]int64)
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan discard item count: %w", err)
		}
		result[state] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate discard item counts: %w", err)
	}
	return result, nil
}

func (records records) Disposition(
	ctx context.Context,
	key importdiscard.Key,
) (importdiscard.Disposition, bool, error) {
	var result importdiscard.Disposition
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT state,error_code FROM import_batch_discards WHERE kind=? AND import_id=?`,
		key.Kind,
		key.ID,
	).
		Scan(&result.State, &result.ErrorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, fmt.Errorf("read discard disposition: %w", err)
	}
	return result, true, nil
}

func (records records) Pending(ctx context.Context) (importdiscard.Request, bool, error) {
	var result importdiscard.Request
	err := records.executor.QueryRowContext(ctx, `SELECT kind,import_id,requested_by_user_id FROM import_batch_discards
WHERE state='REQUESTED' ORDER BY updated_at_ms,kind,import_id LIMIT 1`).Scan(&result.Kind, &result.ID, &result.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, fmt.Errorf("read pending discard: %w", err)
	}
	return result, true, nil
}
