package gamerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	"retrom/internal/persistence/sessionstore"
)

type Records struct{ Executor dbapi.Executor }

func (records Records) StopRuntime(ctx context.Context, gameID string, now int64) error {
	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(ctx, "launch_sessions",
		sessionstore.ChangeLaunch, recordstore.Update{
			Set: `
state='REVOKED',finished_at_ms=COALESCE(finished_at_ms,?),updated_at_ms=?,version=version+1
`, Scope: recordstore.Scope{
				Where: `game_id=? AND state IN ('CREATED','ACTIVE')`,
				Args:  []any{gameID},
			},
			Values: []any{now, now},
		}); err != nil {
		return fmt.Errorf("payloadrelease/revoke launches: %w", err)
	}
	if err := (releaseops.Records{Executor: records.Executor}).ExecUpdate(ctx, "play_sessions",
		"game_id=? AND state='ACTIVE'", []any{gameID}, `
UPDATE play_sessions SET state='ABANDONED',ended_at_ms=?,updated_at_ms=?,version=version+1
WHERE game_id=? AND state='ACTIVE'
`, now, now, gameID); err != nil {
		return fmt.Errorf("payloadrelease/end play sessions: %w", err)
	}
	if err := (releaseops.Records{Executor: records.Executor}).CheckedUpdate(ctx, "launch_sessions",
		sessionstore.ChangeLaunch, recordstore.Update{
			Set: `save_state_id=NULL`,
			Scope: recordstore.Scope{
				Where: `game_id=? AND save_state_id IS NOT NULL`,
				Args:  []any{gameID},
			},
		}); err != nil {
		return fmt.Errorf("payloadrelease/unlink launch saves: %w", err)
	}
	return nil
}
