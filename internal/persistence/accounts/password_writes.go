package accounts

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/accounts"
)

func (records passwordRecords) Rotate(ctx context.Context, plan accounts.PasswordPlan) error {
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE users SET session_version=session_version+1,version=version+1,updated_at_ms=?
 WHERE id=? AND status='ENABLED' AND session_version=?`,
		plan.Now,
		plan.Actor.UserID,
		plan.Actor.SessionVersion,
	)
	if err := authChanged(result, err, accounts.ErrAuthenticationNeeded); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `UPDATE user_credentials SET password_hash=?,password_changed_at_ms=?
 WHERE user_id=? AND password_hash=?`, plan.NewHash, plan.Now, plan.Actor.UserID, plan.ExpectedHash)
	if err := authChanged(result, err, accounts.ErrAuthenticationNeeded); err != nil {
		return err
	}
	if err := records.revokePasswordSessions(ctx, plan); err != nil {
		return err
	}
	if plan.ClearTestDefault {
		if _, err := recordstore.UpdateInstanceState(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `test_default_password_active=0,version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `id=1 AND test_default_password_active=1`,
				},
				Values: []any{
					plan.Now,
				},
			},
		); err != nil {
			return fmt.Errorf("clear default credential flag: %w", err)
		}
	}
	if err := authRecords(records).insertSession(ctx, plan.Session); err != nil {
		return err
	}
	_, err = records.executor.ExecContext(
		ctx,
		`INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms)
 VALUES(?,'USER',?,NULL,'PASSWORD_CHANGED','USER',?,?,?,'{}',NULL,?)`,
		plan.AuditID,
		plan.Actor.UserID,
		plan.Actor.UserID,
		plan.BeforeJSON,
		plan.AfterJSON,
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("audit password rotation: %w", err)
	}
	return nil
}

func (records passwordRecords) revokePasswordSessions(ctx context.Context, plan accounts.PasswordPlan) error {
	_, err := records.executor.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='PASSWORD_CHANGED'
 WHERE user_id=? AND revoked_at_ms IS NULL`, plan.Now, plan.Actor.UserID)
	if err != nil {
		return fmt.Errorf("revoke password-change sessions: %w", err)
	}
	_, err = recordstore.UpdateAccountLinks(ctx, records.executor, recordstore.Update{
		Set: `revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1`, Scope: recordstore.Scope{
			Where: `kind='PASSWORD_RESET' AND target_user_id=? AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL
 AND expires_at_ms>?`,
			Args: []any{
				plan.Actor.UserID,
				plan.Now,
			},
		}, Values: []any{
			plan.Now,
		},
	})
	if err != nil {
		return fmt.Errorf("revoke password-reset links: %w", err)
	}
	return nil
}
