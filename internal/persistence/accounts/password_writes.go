package accounts

import (
	"context"
	"fmt"

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
		if err := authRecords(records).clearDefaultCredential(ctx, plan.Now); err != nil {
			return err
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
	return authRecords(records).revokeCredentialSecurity(ctx, plan.Actor.UserID, "PASSWORD_CHANGED", plan.Now)
}
