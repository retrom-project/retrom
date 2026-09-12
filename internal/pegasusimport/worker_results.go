package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"
)

func (service *Service) closeItem(ctx context.Context, unit work, itemID, state, code string, retryable bool) {
	service.closeItemWithFailure(ctx, unit, itemID, state, code, retryable, nil)
}

func (service *Service) closeItemWithFailure(
	ctx context.Context,
	unit work,
	itemID, state, code string,
	retryable bool,
	failure *FailureDetails,
) {
	outcome := application.ItemOutcome{State: state, Code: code, Retryable: retryable, Failure: failure}
	service.finishItem(ctx, unit, itemID, outcome)
}

func (service *Service) finishItem(ctx context.Context, unit work, itemID string, outcome application.ItemOutcome) {
	items := application.NewItemWork(repository.NewItemWork(service.database), service.now)
	if err := items.Finish(ctx, unit.Identity(), itemID, outcome); err != nil {
		slog.Error("Pegasus item completion failed", "error", service.sanitizeTechnicalDetail(err))
	}
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
