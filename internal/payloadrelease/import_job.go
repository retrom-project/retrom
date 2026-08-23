package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
)

func (service *Service) releaseImportJob(ctx context.Context, job claimedJob) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/import job transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	complete, err := ensureImportJobRelease(ctx, transaction, job)
	if err != nil || complete {
		return err
	}
	if err := service.releaseImportChildren(ctx, transaction, job.ScopeID); err != nil {
		return err
	}
	if err := service.releaseImportAggregate(ctx, transaction, job.ScopeID); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("payloadrelease/import job commit: %w", err)
	}
	return nil
}

func ensureImportJobRelease(
	ctx context.Context,
	transaction *sql.Tx,
	job claimedJob,
) (bool, error) {
	var state, payloadState string
	var version int64
	var releaseJob sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT state,version,payload_state,payload_release_job_id FROM import_jobs WHERE id=?
`, job.ScopeID).Scan(&state, &version, &payloadState, &releaseJob)
	if errors.Is(err, sql.ErrNoRows) {
		return false, releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if err != nil {
		return false, fmt.Errorf("payloadrelease/read import job: %w", err)
	}
	if !terminalImportJob(state) || !releaseJob.Valid || releaseJob.String != job.ID {
		return false, releaseFailure("PAYLOAD_RELEASE_SCOPE_NOT_TERMINAL")
	}
	if version != job.Input.Inputs.ScopeVersion && payloadState != "RELEASED" {
		return false, releaseFailure("PAYLOAD_RELEASE_SCOPE_VERSION_MISMATCH")
	}
	if payloadState == "RELEASED" {
		return true, nil
	}
	if payloadState == "FAILED" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET payload_state='RELEASING',payload_last_error_code=NULL WHERE id=?
`, job.ScopeID); err != nil {
			return false, fmt.Errorf("payloadrelease/retry import job: %w", err)
		}
	}
	return false, nil
}

func (service *Service) releaseImportAggregate(
	ctx context.Context,
	transaction *sql.Tx,
	importJobID string,
) error {
	now := service.now().UnixMilli()
	sessions, err := collectIDs(ctx, transaction, `
SELECT upload_session_id FROM upload_consumptions WHERE consumer_type='IMPORT_JOB' AND consumer_id=?
`, importJobID)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE upload_consumptions SET released_at_ms=?,release_reason='IMPORT_JOB_TERMINAL',version=version+1
WHERE consumer_type='IMPORT_JOB' AND consumer_id=? AND released_at_ms IS NULL
`, now, importJobID); err != nil {
		return fmt.Errorf("payloadrelease/release import aggregate: %w", err)
	}
	purged, err := service.purgeEligibleUploads(ctx, transaction, sessions, now)
	if err != nil {
		return err
	}
	if err := service.stageCandidates(ctx, transaction, purged); err != nil {
		return err
	}
	var remains int
	if err := transaction.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM import_items WHERE import_job_id=? AND payload_state<>'RELEASED')+
       (SELECT count(*) FROM upload_consumptions
        WHERE consumer_type='IMPORT_JOB' AND consumer_id=? AND released_at_ms IS NULL)
`, importJobID, importJobID).Scan(&remains); err != nil || remains != 0 {
		return releaseFailure("PAYLOAD_RELEASE_REFERENCE_REMAINS")
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_jobs SET payload_state='RELEASED',payload_released_at_ms=?,payload_last_error_code=NULL
WHERE id=? AND payload_state IN ('RELEASING','FAILED','RELEASED')
`, now, importJobID); err != nil {
		return fmt.Errorf("payloadrelease/complete import job: %w", err)
	}
	return nil
}

func (service *Service) releaseImportChildren(ctx context.Context, transaction *sql.Tx, importJobID string) error {
	items, err := collectIDs(ctx, transaction, `
SELECT id FROM import_items WHERE import_job_id=? ORDER BY id
`, importJobID)
	if err != nil {
		return err
	}
	for _, itemID := range items {
		var state, payloadState string
		var releaseJob sql.NullString
		if err := transaction.QueryRowContext(ctx, `
SELECT state,payload_state,payload_release_job_id FROM import_items WHERE id=?
`, itemID).Scan(&state, &payloadState, &releaseJob); err != nil {
			return fmt.Errorf("payloadrelease/read import child: %w", err)
		}
		if !terminalImportItem(state) || payloadState == "RETAINED" || !releaseJob.Valid {
			return releaseFailure("PAYLOAD_RELEASE_DEPENDENCY_PENDING")
		}
		if payloadState != "RELEASED" {
			if err := service.releaseImportChild(ctx, transaction, itemID, state, payloadState); err != nil {
				return err
			}
		}
	}
	return nil
}

func (service *Service) releaseImportChild(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, state, payloadState string,
) error {
	if payloadState == "FAILED" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET payload_state='RELEASING',payload_last_error_code=NULL
WHERE id=?
`, itemID); err != nil {
			return fmt.Errorf("payloadrelease/retry import child: %w", err)
		}
	}
	if err := service.releaseImportItemTx(
		ctx, transaction, itemID, reasonForImportState(state), service.now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("payloadrelease/release import child: %w", err)
	}
	return nil
}

func terminalImportJob(state string) bool {
	switch state {
	case "COMPLETED", "CANCELLED", "FAILED":
		return true
	default:
		return false
	}
}
