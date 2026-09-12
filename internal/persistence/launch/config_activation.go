package launch

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	application "retrom/internal/service/launch"
)

func (records configRecords) Activate(ctx context.Context, plan application.ConfigActivationPlan) error {
	change := recordstore.Update{
		Set:    `state='ACTIVE',activated_at_ms=?,updated_at_ms=?,version=version+1`,
		Values: []any{plan.NowMS, plan.NowMS},
		Scope: recordstore.Scope{
			Where: `id=? AND state='CREATED' AND version=? AND bootstrap_expires_at_ms>? AND hard_expires_at_ms>?`,
			Args:  []any{plan.Ref.ID, plan.Version, plan.NowMS, plan.NowMS},
		},
	}
	write := sessionstore.ChangeLaunch
	if plan.Ref.Preview {
		write = sessionstore.ChangePreview
	}
	result, err := write(ctx, records.executor, change)
	if err != nil {
		return fmt.Errorf("activate config source: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read config activation count: %w", err)
	}
	if count != 1 {
		return application.ErrCredential
	}
	return nil
}
