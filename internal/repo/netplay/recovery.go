package netplay

import (
	"context"
	"fmt"

	"retrom/internal/model/netplay"
	"retrom/internal/repo/recordstore"
	"retrom/internal/repo/sessionstore"
)

func (records roomMaintenanceRecords) Recover(ctx context.Context, plan netplay.RecoveryPlan) error {
	now, reason := plan.Now, plan.Reason
	if _, err := recordstore.UpdateNetplaySessions(ctx, records.executor, recordstore.Update{
		Set: `state='FAILED',finished_at_ms=?,end_reason=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `state NOT IN ('FINISHED','FAILED')`,
		},
		Values: []any{now, reason, now},
	}); err != nil {
		return fmt.Errorf("netplay/recover sessions: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `
UPDATE play_sessions
SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE launch_session_id IN (
  SELECT id FROM launch_sessions WHERE netplay_session_id IS NOT NULL
)
AND state='ACTIVE'
`, now, now); err != nil {
		return fmt.Errorf("netplay/recover plays: %w", err)
	}
	if _, err := sessionstore.ChangeLaunch(ctx, records.executor, recordstore.Update{
		Set: `state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `netplay_session_id IS NOT NULL AND state IN ('CREATED','ACTIVE')`,
		},
		Values: []any{now, now},
	}); err != nil {
		return fmt.Errorf("netplay/recover launches: %w", err)
	}
	if _, err := recordstore.UpdateNetplayRooms(ctx, records.executor, recordstore.Update{
		Set: `
state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason=?,
updated_at_ms=?,version=version+1
`,
		Scope: recordstore.Scope{
			Where: `state IN ('STARTING','RUNNING')`,
		},
		Values: []any{now, reason, now},
	}); err != nil {
		return fmt.Errorf("netplay/recover rooms: %w", err)
	}
	return nil
}
