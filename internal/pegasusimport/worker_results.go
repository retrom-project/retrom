package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
)

func (service *Service) attachLibraryResult(
	ctx context.Context,
	itemID, importJobID string,
	imported libraryimport.ServerImportItem,
) error {
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_items
SET execution_state='VALIDATING',content_kind=?,source_manifest_json=?,source_manifest_digest=?,
library_import_job_id=?,library_import_item_id=?,updated_at_ms=?
WHERE id=? AND execution_state='COPYING'`,
		imported.ContentKind,
		imported.SourceManifestJSON,
		imported.SourceManifestDigest,
		importJobID,
		imported.ItemID,
		now,
		itemID,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/attach library result: %w", err)
	}
	if rowsAffected(result) != 1 {
		return fmt.Errorf("pegasusimport/attach library result: %w", errItemStateChanged)
	}
	return nil
}

func (service *Service) closeItem(
	ctx context.Context,
	itemID, state, code string,
	retryable bool,
	existingGameID string,
) {
	service.closeItemWithFailure(ctx, itemID, state, code, retryable, existingGameID, nil)
}

func (service *Service) closeItemWithFailure(
	ctx context.Context,
	itemID, state, code string,
	retryable bool,
	existingGameID string,
	failure *FailureDetails,
) {
	now := service.now().UnixMilli()
	var encodedFailure any
	if failure != nil {
		if encoded, err := json.Marshal(failure); err == nil {
			encodedFailure = string(encoded)
		}
	}
	setExisting := any(nil)
	var revision any
	if existingGameID != "" {
		setExisting = existingGameID
		var value string
		if err := service.database.QueryRowContext(
			ctx, `SELECT current_content_revision_id FROM games WHERE id=?`, existingGameID,
		).Scan(&value); err == nil {
			revision = value
		}
	}
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_items
SET execution_state=?,error_code=?,retryable=?,
error_details_json=?,
existing_game_id=COALESCE(?,existing_game_id),
existing_content_revision_id=COALESCE(?,existing_content_revision_id),
completed_at_ms=?,updated_at_ms=?
WHERE id=? AND execution_state IN ('COPYING','VALIDATING','PUBLISHING')`,
		state,
		nullIfEmpty(code),
		boolInt(retryable),
		encodedFailure,
		setExisting,
		revision,
		now,
		now,
		itemID,
	)
}

func (service *Service) closeAssetWarning(ctx context.Context, itemID, kind, code string) {
	now := service.now().UnixMilli()
	state := "READ_FAILED"
	if code == "PEGASUS_SOURCE_CHANGED" {
		state = "SOURCE_CHANGED"
	}
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_item_assets SET state=?,warning_code=?,updated_at_ms=? WHERE item_id=? AND kind=?`,
		state,
		code,
		now,
		itemID,
		kind,
	)
	var encoded string
	if err := service.database.QueryRowContext(
		ctx, `SELECT warnings_json FROM pegasus_import_items WHERE id=?`, itemID,
	).Scan(&encoded); err != nil {
		return
	}
	warnings := make([]map[string]any, 0, 1)
	_ = json.Unmarshal([]byte(encoded), &warnings)
	warnings = append(warnings, map[string]any{"code": code, "field": strings.ToLower(kind)})
	encodedBytes, _ := json.Marshal(warnings)
	_, _ = service.database.ExecContext(
		ctx,
		`UPDATE pegasus_import_items SET warnings_json=?,updated_at_ms=? WHERE id=?`,
		string(encodedBytes),
		now,
		itemID,
	)
}

func mediaWarning(kind string, err error) string {
	if errors.Is(err, ErrSourceChanged) {
		return "PEGASUS_SOURCE_CHANGED"
	}
	if kind == "COVER" {
		return "PEGASUS_IMAGE_INVALID"
	}
	return "PEGASUS_VIDEO_UNSUPPORTED"
}

func (service *Service) refreshCountsAndEvent(
	ctx context.Context,
	transaction *sql.Tx,
	unit work,
	itemID, outcome string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET review_pending_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='REVIEW_PENDING'
),
published_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='PUBLISHED'
),
review_discarded_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='REVIEW_DISCARDED'
),
existing_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='SKIPPED_EXISTING'
),
blocked_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION')
),
failed_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
cancelled_item_count=(
  SELECT count(*) FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
media_warning_count=(
  SELECT count(*)
  FROM pegasus_import_items item,json_each(item.warnings_json) warning
  WHERE item.import_id=?
  AND json_extract(warning.value,'$.code') IN (
    'PEGASUS_IMAGE_INVALID','PEGASUS_VIDEO_UNSUPPORTED','PEGASUS_VIDEO_TOO_LARGE',
    'PEGASUS_MEDIA_AMBIGUOUS','PEGASUS_MEDIA_MISSING','PEGASUS_MEDIA_READ_FAILED'
  )
),
version=version+1,updated_at_ms=?
WHERE id=?`, unit.ImportID, unit.ImportID, unit.ImportID, unit.ImportID, unit.ImportID,
		unit.ImportID, unit.ImportID, unit.ImportID, now, unit.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/refresh aggregate counts: %w", err)
	}
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "itemId": itemID, "outcome": outcome})
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'PROGRESS',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(data),
		now,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/create progress event: %w", err)
	}
	return nil
}

func (service *Service) closeCancelled(ctx context.Context, unit work) (bool, error) {
	var state string
	if err := service.database.QueryRowContext(
		ctx, `SELECT state FROM pegasus_imports WHERE id=?`, unit.ImportID,
	).Scan(&state); err != nil {
		return false, fmt.Errorf("pegasusimport/read cancellation state: %w", err)
	}
	if state != "CANCEL_REQUESTED" {
		return false, nil
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("pegasusimport/start cancellation close: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,updated_at_ms=?
WHERE import_id=? AND execution_state='PENDING'`, now, now, unit.ImportID); err != nil {
		return false, fmt.Errorf("pegasusimport/cancel remaining items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state='CANCELLED',phase=NULL,
cancelled_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, unit.ImportID, now, now, unit.ImportID); err != nil {
		return false, fmt.Errorf("pegasusimport/close cancelled import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=?
WHERE id=?`, now, now, unit.JobID); err != nil {
		return false, fmt.Errorf("pegasusimport/close cancelled job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'CANCELLED','{"schemaVersion":1}',?)`,
		unit.JobID, unit.ImportID, now); err != nil {
		return false, fmt.Errorf("pegasusimport/create cancelled event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("pegasusimport/commit cancellation: %w", err)
	}
	return true, nil
}

func (service *Service) finishImport(ctx context.Context, unit work) error {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start finish transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var blocked, failed, reviewPending, published, reviewDiscarded, existing, cancelled int64
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FILTER(
  WHERE execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION')
),
count(*) FILTER(
  WHERE execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')
),
count(*) FILTER(WHERE execution_state='REVIEW_PENDING'),
count(*) FILTER(WHERE execution_state='PUBLISHED'),
count(*) FILTER(WHERE execution_state='REVIEW_DISCARDED'),
count(*) FILTER(WHERE execution_state='SKIPPED_EXISTING'),
count(*) FILTER(WHERE execution_state='CANCELLED')
FROM pegasus_import_items
WHERE import_id=?`, unit.ImportID).
		Scan(&blocked, &failed, &reviewPending, &published, &reviewDiscarded, &existing, &cancelled); err != nil {
		return fmt.Errorf("pegasusimport/read final counts: %w", err)
	}
	state := "COMPLETED"
	if blocked+failed > 0 {
		state = "PARTIAL_FAILURE"
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state=?,phase=NULL,review_pending_item_count=?,published_item_count=?,
review_discarded_item_count=?,existing_item_count=?,
blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,retryable=?,
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, state, reviewPending, published, reviewDiscarded, existing, blocked, failed, cancelled,
		boolInt(failed > 0), now, now, unit.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/finish import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=?
WHERE id=?`, now, now, unit.JobID); err != nil {
		return fmt.Errorf("pegasusimport/finish job: %w", err)
	}
	data, _ := json.Marshal(
		map[string]any{
			"schemaVersion":   1,
			"state":           state,
			"reviewPending":   reviewPending,
			"published":       published,
			"reviewDiscarded": reviewDiscarded,
			"existing":        existing,
			"blocked":         blocked,
			"failed":          failed,
		},
	)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SUCCEEDED',?,?)`,
		unit.JobID, unit.ImportID, string(data), now); err != nil {
		return fmt.Errorf("pegasusimport/create success event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit finish: %w", err)
	}
	return nil
}
