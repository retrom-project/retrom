package netplay

import (
	"context"
	"errors"
	"fmt"
)

var errInvalidRecoveryReason = errors.New("netplay/recovery: invalid reason")

func (service *Service) Recover(ctx context.Context, reason string) error {
	if reason != "SERVER_RESTARTED" && reason != "RESTORE" {
		return errInvalidRecoveryReason
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/recovery: %w", err)
	}
	defer transaction.Rollback() //nolint:errcheck // Commit is the authoritative result.
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='FAILED',finished_at_ms=?,end_reason=?,updated_at_ms=?,version=version+1
WHERE state NOT IN ('FINISHED','FAILED')
`, now, reason, now); err != nil {
		return fmt.Errorf("netplay/recover sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE netplay_session_id IS NOT NULL AND state IN ('CREATED','ACTIVE')
`, now, now); err != nil {
		return fmt.Errorf("netplay/recover launches: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason=?,
updated_at_ms=?,version=version+1
WHERE state IN ('STARTING','RUNNING')
`, now, reason, now); err != nil {
		return fmt.Errorf("netplay/recover rooms: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("netplay/recovery: %w", err)
	}
	return nil
}
