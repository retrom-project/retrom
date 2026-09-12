package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/recordstore"
)

func ScheduleTerminalImportItem(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	reason Reason,
	now int64,
) (string, error) {
	return scheduleTerminalOwner(ctx, transaction, terminalOwnerRequest{
		id: itemID, scope: ScopeImportItem, reason: reason, now: now,
		readQuery: `SELECT state,version,payload_state,payload_release_job_id FROM import_items WHERE id=?`,
		terminal:  terminalImportItem, label: "import item",
	})
}

func ScheduleTerminalImportJob(ctx context.Context, transaction *sql.Tx, importID string, now int64) (string, error) {
	var pending int
	if err := transaction.QueryRowContext(ctx, `SELECT count(*) FROM import_items
WHERE import_job_id=? AND state NOT IN ('PUBLISHED','DISCARDED','FAILED_FINAL','CANCELLED')`, importID).
		Scan(&pending); err != nil {
		return "", fmt.Errorf("payloadrelease/check import children: %w", err)
	}
	if pending > 0 {
		return "", nil
	}
	return scheduleTerminalOwner(ctx, transaction, terminalOwnerRequest{
		id: importID, scope: ScopeImportJob, reason: ReasonImportTerminal, now: now,
		readQuery: `SELECT state,version,payload_state,payload_release_job_id FROM import_jobs WHERE id=?`,
		terminal:  terminalImportJob, ignoreNonTerminal: true, label: "import job",
	})
}

type terminalOwnerRequest struct {
	id, readQuery, label string
	scope                ScopeType
	reason               Reason
	now                  int64
	terminal             func(string) bool
	ignoreNonTerminal    bool
}

func scheduleTerminalOwner(
	ctx context.Context,
	transaction *sql.Tx,
	request terminalOwnerRequest,
) (string, error) {
	var state, payloadState string
	var version int64
	var existing sql.NullString
	if err := transaction.QueryRowContext(ctx, request.readQuery, request.id).
		Scan(&state, &version, &payloadState, &existing); err != nil {
		return "", fmt.Errorf("payloadrelease/schedule %s: %w", request.label, err)
	}
	if !request.terminal(state) {
		if request.ignoreNonTerminal {
			return "", nil
		}
		return "", ErrScopeInvalid
	}
	if payloadState != "RETAINED" {
		if existing.Valid {
			return existing.String, nil
		}
		return "", ErrScopeInvalid
	}
	jobID, err := Schedule(ctx, transaction, request.scope, request.id, version+1, request.reason, request.now)
	if err != nil {
		return "", fmt.Errorf("payloadrelease/schedule %s job: %w", request.label, err)
	}
	result, err := updatePayloadOwner(ctx, transaction, request.scope, recordstore.Update{
		Set:    "payload_state='RELEASING',payload_release_job_id=?,version=version+1",
		Values: []any{jobID},
		Scope: recordstore.Scope{
			Where: "id=? AND version=? AND payload_state='RETAINED'",
			Args:  []any{request.id, version},
		},
	})
	if err != nil {
		return "", fmt.Errorf("payloadrelease/enter %s release: %w", request.label, err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return "", ErrScopeInvalid
	}
	return jobID, nil
}

func ScheduleTerminalPegasusItem(ctx context.Context, transaction *sql.Tx, itemID string, now int64) (string, error) {
	return scheduleTerminalSourceImportItem(ctx, transaction, itemID, now, pegasusSourceImportItem)
}

func ScheduleTerminalEmulationStationItem(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
) (string, error) {
	return scheduleTerminalSourceImportItem(
		ctx, transaction, itemID, now, emulationStationSourceImportItem,
	)
}

func scheduleTerminalSourceImportItem(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
	spec sourceImportItemSpec,
) (string, error) {
	var state, payloadState string
	var retryable bool
	var version int64
	var publicItem, existing sql.NullString
	query := fmt.Sprintf(`
SELECT execution_state,retryable,version,payload_state,library_import_item_id,payload_release_job_id
FROM %s WHERE id=?
`, spec.itemsTable)
	if err := transaction.QueryRowContext(ctx, query, itemID).
		Scan(&state, &retryable, &version, &payloadState, &publicItem, &existing); err != nil {
		return "", fmt.Errorf("payloadrelease/schedule %s item: %w", spec.label, err)
	}
	if !spec.terminal(state, retryable) {
		return "", nil
	}
	if payloadState != "RETAINED" {
		if existing.Valid {
			return existing.String, nil
		}
		return "", ErrScopeInvalid
	}
	if publicItem.Valid {
		return linkSourceImportItemRelease(ctx, transaction, itemID, publicItem.String, spec)
	}
	jobID, err := Schedule(ctx, transaction, spec.scope, itemID, version+1, spec.reason, now)
	if err != nil {
		return "", err
	}
	updateQuery := recordstore.Update{
		Set:    `payload_state='RELEASING',payload_release_job_id=?,version=version+1`,
		Values: []any{jobID},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND payload_state='RETAINED'`,
			Args:  []any{itemID, version},
		},
	}
	_, err = spec.updateItem(ctx, transaction, updateQuery)
	if err != nil {
		return "", fmt.Errorf("payloadrelease/enter %s release: %w", spec.label, err)
	}
	return jobID, nil
}

func linkSourceImportItemRelease(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	publicItemID string,
	spec sourceImportItemSpec,
) (string, error) {
	var sharedJob string
	if err := transaction.QueryRowContext(ctx, `
SELECT payload_release_job_id FROM import_items
WHERE id=? AND payload_state IN ('RELEASING','RELEASED','FAILED')
`, publicItemID).Scan(&sharedJob); err != nil {
		return "", fmt.Errorf("payloadrelease/link %s schedule: %w", spec.label, err)
	}
	query := recordstore.Update{
		Set:    `payload_state='RELEASING',payload_release_job_id=?,version=version+1`,
		Values: []any{sharedJob},
		Scope: recordstore.Scope{
			Where: `id=? AND payload_state='RETAINED'`,
			Args:  []any{itemID},
		},
	}
	if _, err := spec.updateItem(ctx, transaction, query); err != nil {
		return "", fmt.Errorf("payloadrelease/link %s release: %w", spec.label, err)
	}
	return sharedJob, nil
}

func ScheduleConsumption(ctx context.Context, transaction *sql.Tx, consumptionID string, now int64) (string, error) {
	var version int64
	var released sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT version,released_at_ms FROM upload_consumptions WHERE id=?
`, consumptionID).
		Scan(&version, &released); err != nil {
		return "", fmt.Errorf("payloadrelease/schedule consumption: %w", err)
	}
	if released.Valid {
		return "", nil
	}
	var existing string
	err := transaction.QueryRowContext(ctx, `
SELECT id FROM jobs WHERE kind='PAYLOAD_RELEASE' AND scope_type='UPLOAD_CONSUMPTION' AND scope_id=?
`, consumptionID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("payloadrelease/find consumption release: %w", err)
	}
	return Schedule(ctx, transaction, ScopeUploadConsumption, consumptionID, version, ReasonUploadConsumed, now)
}

func ScheduleGameDeletion(
	ctx context.Context,
	transaction *sql.Tx,
	gameID string,
	version int64,
	now int64,
) (string, error) {
	jobID, err := Schedule(ctx, transaction, ScopeGame, gameID, version+1, ReasonGameDeleted, now)
	if err != nil {
		return "", err
	}
	result, err := recordstore.UpdateGames(ctx, transaction, recordstore.Update{
		Set: `
status='DELETED',payload_state='RELEASING',payload_release_job_id=?,deleted_at_ms=?,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND status='PUBLISHED' AND version=?`,
			Args:  []any{gameID, version},
		},
		Values: []any{jobID, now, now},
	})
	if err != nil {
		return "", fmt.Errorf("payloadrelease/delete game: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return "", ErrScopeInvalid
	}
	return jobID, nil
}
