package accounts

import (
	"context"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/recordstore"
)

func (records linkRecords) Reset(ctx context.Context, plan accounts.ResetConsumption) error {
	target := plan.Target
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE users SET session_version=session_version+1,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND session_version=? AND status!='DELETED'`,
		plan.Now,
		target.User.UserID,
		target.Version,
		target.SessionVersion,
	)
	if err := authChanged(result, err, accounts.ErrAccountLinkUnavailable); err != nil {
		return err
	}
	result, err = recordstore.UpdateAccountLinks(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `consumed_at_ms=?,consumed_by_user_id=?,version=version+1`,
			Scope: recordstore.Scope{
				Where: `id=? AND kind='PASSWORD_RESET' AND target_user_id=? AND version=?
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?`,
				Args: []any{
					plan.LinkID,
					target.User.UserID,
					plan.LinkVersion,
					plan.Now,
				},
			},
			Values: []any{
				plan.Now,
				target.User.UserID,
			},
		},
	)
	if err := authChanged(result, err, accounts.ErrAccountLinkUnavailable); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(
		ctx,
		`UPDATE user_credentials SET password_hash=?,password_scheme='ARGON2ID_V1',password_changed_at_ms=? WHERE user_id=?`,
		plan.PasswordHash,
		plan.Now,
		target.User.UserID,
	)
	if err := authChanged(result, err, accounts.ErrAccountLinkUnavailable); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='PASSWORD_RESET' WHERE user_id=? AND revoked_at_ms IS NULL`,
		plan.Now,
		target.User.UserID,
	); err != nil {
		return fmt.Errorf("revoke password reset sessions: %w", err)
	}
	security := authRecords{records.executor}
	if plan.ClearTestDefault {
		if err := security.clearDefaultCredential(ctx, plan.Now); err != nil {
			return err
		}
	}
	if plan.Session != nil {
		if err := security.insertSession(ctx, *plan.Session); err != nil {
			return err
		}
	}
	return nil
}
