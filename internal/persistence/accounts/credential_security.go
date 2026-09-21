package accounts

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
)

func (records authRecords) clearDefaultCredential(ctx context.Context, now int64) error {
	_, err := recordstore.UpdateInstanceState(ctx, records.executor, recordstore.Update{
		Set: `test_default_password_active=0,version=version+1,updated_at_ms=?`, Scope: recordstore.Scope{
			Where: `id=1 AND test_default_password_active=1`,
		}, Values: []any{
			now,
		},
	})
	if err != nil {
		return fmt.Errorf("clear default credential flag: %w", err)
	}
	return nil
}

func (records authRecords) revokeCredentialSecurity(ctx context.Context, userID, reason string, now int64) error {
	_, err := records.executor.ExecContext(
		ctx,
		`UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason=? WHERE user_id=? AND revoked_at_ms IS NULL`,
		now,
		reason,
		userID,
	)
	if err != nil {
		return fmt.Errorf("revoke credential sessions: %w", err)
	}
	_, err = recordstore.UpdateAccountLinks(ctx, records.executor, recordstore.Update{
		Set: `revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1`, Scope: recordstore.Scope{
			Where: `kind='PASSWORD_RESET' AND target_user_id=? AND consumed_at_ms IS NULL
 AND revoked_at_ms IS NULL AND expires_at_ms>?`,
			Args: []any{
				userID,
				now,
			},
		}, Values: []any{
			now,
		},
	})
	if err != nil {
		return fmt.Errorf("revoke credential reset links: %w", err)
	}
	return nil
}
