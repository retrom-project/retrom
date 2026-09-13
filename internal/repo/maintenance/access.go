package maintenance

import (
	"context"
	"fmt"

	"retrom/internal/repo/recordstore"
	"retrom/internal/repo/sessionstore"
	"retrom/internal/service/maintenance"
)

func (writes writes) RevokeAccess(ctx context.Context, nowMS int64) (maintenance.AccessCounts, error) {
	transaction := writes.transaction
	sessions, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='RESTORE' WHERE revoked_at_ms IS NULL
`, nowMS)
	if err != nil {
		return maintenance.AccessCounts{}, fmt.Errorf("maintenance/bundle: fence restored sessions: %w", err)
	}
	links, err := recordstore.UpdateAccountLinks(ctx, transaction, recordstore.Update{
		Set: `revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1`,
		Scope: recordstore.Scope{
			Where: `consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?`,
			Args:  []any{nowMS},
		},
		Values: []any{nowMS},
	})
	if err != nil {
		return maintenance.AccessCounts{}, fmt.Errorf("maintenance/bundle: fence restored account links: %w", err)
	}
	launches, err := sessionstore.ChangeLaunch(ctx, transaction, recordstore.Update{
		Set: `state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1`,
		Scope: recordstore.Scope{
			Where: `state IN ('CREATED','ACTIVE')`,
		},
		Values: []any{nowMS, nowMS},
	})
	if err != nil {
		return maintenance.AccessCounts{}, fmt.Errorf("maintenance/bundle: fence restored launches: %w", err)
	}

	counts, err := affected(sessions, links, launches)
	if err != nil {
		return maintenance.AccessCounts{}, err
	}
	return maintenance.AccessCounts{Sessions: counts[0], Links: counts[1], Launches: counts[2]}, nil
}
