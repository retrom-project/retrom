package pegasusimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/libraryimport"
	"retrom/internal/payloadrelease"
)

func (service *Service) attachLibraryResult(
	ctx context.Context,
	itemID, importJobID string,
	imported libraryimport.ServerImportItem,
) error {
	now := service.now().UnixMilli()
	result, err := recordstore.UpdatePegasusImportItems(ctx, service.database, recordstore.Update{
		Set: `
execution_state='VALIDATING',content_kind=?,source_manifest_json=?,source_manifest_digest=?,
library_import_job_id=?,library_import_item_id=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND execution_state='COPYING'`,
			Args:  []any{itemID},
		},
		Values: []any{
			imported.ContentKind,
			imported.SourceManifestJSON,
			imported.SourceManifestDigest,
			importJobID,
			imported.ItemID,
			now,
		},
	})
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
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer dbexec.Rollback(transaction)
	result, err := recordstore.UpdatePegasusImportItems(ctx, transaction, recordstore.Update{
		Set: `
execution_state=?,error_code=?,retryable=?,
error_details_json=?,
existing_game_id=COALESCE(?,existing_game_id),
completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND execution_state IN ('COPYING','VALIDATING')`,
			Args:  []any{itemID},
		},
		Values: []any{
			state,
			nullIfEmpty(code),
			boolInt(retryable),
			encodedFailure,
			nullIfEmpty(existingGameID),
			now,
			now,
		},
	})
	if err != nil || rowsAffected(result) != 1 {
		return
	}
	if _, err := payloadrelease.ScheduleTerminalPegasusItem(ctx, transaction, itemID, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) closeAssetWarning(ctx context.Context, itemID, kind, code string) {
	now := service.now().UnixMilli()
	state := "READ_FAILED"
	if code == "PEGASUS_SOURCE_CHANGED" {
		state = "SOURCE_CHANGED"
	}
	_, _ = recordstore.UpdatePegasusImportItemAssets(ctx, service.database, recordstore.Update{
		Set: `state=?,warning_code=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `item_id=? AND kind=?`,
			Args:  []any{itemID, kind},
		},
		Values: []any{state, code, now},
	})
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
	_, _ = recordstore.UpdatePegasusImportItems(ctx, service.database, recordstore.Update{
		Set: `warnings_json=?,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{itemID},
		},
		Values: []any{string(encodedBytes), now},
	})
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
	defer dbexec.Rollback(transaction)
	if _, err := recordstore.UpdatePegasusImportItems(ctx, transaction, recordstore.Update{
		Set: `
execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `import_id=? AND execution_state='PENDING'`,
			Args:  []any{unit.ImportID},
		},
		Values: []any{now, now},
	}); err != nil {
		return false, fmt.Errorf("pegasusimport/cancel remaining items: %w", err)
	}
	if _, err := recordstore.UpdatePegasusImports(ctx, transaction, recordstore.Update{
		Set: `
state='CANCELLED',phase=NULL,
cancelled_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
completed_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=?`,
			Args:  []any{unit.ImportID},
		},
		Values: []any{unit.ImportID, now, now},
	}); err != nil {
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
	if err := scheduleTerminalItems(ctx, transaction, unit.ImportID, now); err != nil {
		return false, err
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("pegasusimport/commit cancellation: %w", err)
	}
	return true, nil
}

func (service *Service) finishImport(ctx context.Context, unit work) error {
	completion := application.NewCompletion(repository.NewCompletion(service.database), service.now)
	if err := completion.Finish(ctx, unit.Identity()); err != nil {
		return fmt.Errorf("pegasusimport/finish: %w", err)
	}
	return nil
}
