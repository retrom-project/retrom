package accounts

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/accounts"
)

func (records recoveryRecords) Reset(ctx context.Context, plan accounts.RecoveryPlan) error {
	result, err := recordstore.UpdateUsers(ctx, records.executor, recordstore.Update{
		Set: `status='ENABLED',session_version=session_version+1,version=version+1,updated_at_ms=?,disabled_at_ms=NULL`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND role='ADMIN' AND status!='DELETED'`,
			Args: []any{
				plan.Target.UserID,
				plan.Target.Version,
			},
		}, Values: []any{
			plan.Now,
		},
	})
	if err := authChanged(result, err, accounts.ErrOfflineAdmin); err != nil {
		return err
	}
	_, err = records.executor.ExecContext(
		ctx,
		`UPDATE user_credentials SET password_hash=?,password_scheme='ARGON2ID_V1',password_changed_at_ms=? WHERE user_id=?`,
		plan.PasswordHash,
		plan.Now,
		plan.Target.UserID,
	)
	if err != nil {
		return fmt.Errorf("replace recovery credential: %w", err)
	}
	security := authRecords(records)
	if err := security.revokeCredentialSecurity(ctx, plan.Target.UserID, "OFFLINE_RECOVERY", plan.Now); err != nil {
		return err
	}
	if plan.ClearTestDefault {
		if err := security.clearDefaultCredential(ctx, plan.Now); err != nil {
			return err
		}
	}
	_, err = records.executor.ExecContext(
		ctx,
		`INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms)
 VALUES(?,'SYSTEM',NULL,'offline-recovery','ADMIN_OFFLINE_RECOVERED','USER',?,?,?,'{}',NULL,?)`,

		plan.AuditID,
		plan.Target.UserID,
		plan.BeforeJSON,
		plan.AfterJSON,
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("audit offline recovery: %w", err)
	}
	return nil
}
