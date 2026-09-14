package importdiscard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/importdiscard"
	payloadpersistence "retrom/internal/repo/payloadrelease"
)

func (writes writes) Unlinked(ctx context.Context, key importdiscard.Key) ([]string, error) {
	table, err := batchTable(key.Kind)
	if err != nil {
		return nil, err
	}
	items := table[:len(table)-1] + "_items"
	id := key.ID
	ids, err := payloadpersistence.CollectScopeIDs(ctx, writes.transaction, `
SELECT id FROM `+items+` WHERE import_id=? AND library_import_job_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM server_import_upload_owners WHERE source_item_id=`+items+`.id)`, id)
	if err != nil {
		return nil, fmt.Errorf("importdiscard/list unlinked sources: %w", err)
	}
	return ids, nil
}

func (writes writes) ImportByUpload(ctx context.Context, id string) (string, error) {
	var result string
	err := writes.transaction.QueryRowContext(
		ctx,
		`SELECT id FROM import_jobs WHERE upload_session_id=?`,
		id,
	).Scan(
		&result,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find source import: %w", err)
	}
	return result, nil
}

func (writes writes) Link(ctx context.Context, kind, itemID, importID string) error {
	_, err := writes.transaction.ExecContext(
		ctx,
		`INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id)
SELECT upload_session_id,?,? FROM import_jobs WHERE id=? ON CONFLICT(upload_session_id) DO NOTHING`,
		kind,
		itemID,
		importID,
	)
	if err != nil {
		return fmt.Errorf("link source upload owner: %w", err)
	}
	return nil
}
