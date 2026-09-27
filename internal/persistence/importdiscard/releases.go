package importdiscard

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"

	dbapi "retrom/internal/database"
	"retrom/internal/service/importdiscard"
)

func (writes writes) Releases(ctx context.Context, key importdiscard.Key) (importdiscard.ReleaseFacts, error) {
	table, err := batchTable(key.Kind)
	if err != nil {
		return importdiscard.ReleaseFacts{}, err
	}
	items := table[:len(table)-1] + "_items"
	var result importdiscard.ReleaseFacts
	err = dbapi.QueryRowContext(ctx, writes.transaction, `SELECT count(*) FROM `+items+` WHERE import_id=?
 AND payload_state ='RELEASING'
 AND execution_state NOT IN ('PUBLISHED','SKIPPED_EXISTING','REVIEW_DISCARDED')`, key.ID).Scan(
		&result.Releasing,
	)
	if err != nil {
		return result, fmt.Errorf("read source release count: %w", err)
	}
	err = dbapi.QueryRowContext(
		ctx, writes.transaction,

		`SELECT count(*) FROM `+items+` item
 JOIN jobs job ON job.id=item.payload_release_job_id WHERE item.import_id=? AND job.state='FAILED'`,
		key.ID,
	).Scan(
		&result.Failed,
	)
	if err != nil {
		return result, fmt.Errorf("read failed source releases: %w", err)
	}
	return result, nil
}

func (writes writes) UnusedUploads(ctx context.Context, key importdiscard.Key) ([]string, error) {
	kind, id := key.Kind, key.ID
	table, err := batchTable(kind)
	if err != nil {
		return nil, err
	}
	ids, err := dbapi.QueryStrings(
		ctx,
		writes.transaction,
		`
SELECT owner.upload_session_id FROM server_import_upload_owners owner
JOIN `+table[:len(table)-1]+`_items item ON item.id=owner.source_item_id
WHERE owner.kind=? AND item.import_id=?
AND NOT EXISTS(SELECT 1 FROM import_jobs WHERE upload_session_id=owner.upload_session_id)
AND NOT EXISTS(SELECT 1 FROM upload_consumptions WHERE upload_session_id=owner.upload_session_id)`,
		kind,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("importdiscard/list unused envelopes: %w", err)
	}
	return ids, nil
}

func (writes writes) DeleteUpload(ctx context.Context, id string) error {
	if _, err := recordstore.DeleteRows(
		ctx,
		writes.transaction,
		"import_files",
		recordstore.Scope{Where: "upload_session_id=?", Args: []any{id}},
	); err != nil {
		return fmt.Errorf("release normalized upload: %w", err)
	}
	if _, err := recordstore.DeleteRows(
		ctx,
		writes.transaction,
		"upload_files",
		recordstore.Scope{Where: "upload_session_id=?", Args: []any{id}},
	); err != nil {
		return fmt.Errorf("delete unused upload files: %w", err)
	}
	if _, err := writes.transaction.ExecContext(ctx, `DELETE FROM upload_sessions WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete unused envelope: %w", err)
	}
	return nil
}
