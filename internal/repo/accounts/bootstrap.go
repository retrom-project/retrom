package accounts

import (
	"context"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/recordstore"
)

func (records initializationRecords) Bootstrap(ctx context.Context, plan accounts.BootstrapPlan) error {
	if _, err := recordstore.CreateProfiles(
		ctx,
		records.executor,
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`,
		plan.ProfileID,
		plan.DisplayName,
		plan.Now,
	); err != nil {
		return fmt.Errorf("create initial profile: %w", err)
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES(?,?,?,?,'ADMIN','ENABLED',?,?)`,
		plan.UserID,
		plan.ProfileID,
		plan.Username,
		plan.DisplayName,
		plan.Now,
		plan.Now,
	); err != nil {
		return fmt.Errorf("create initial user: %w", err)
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
 VALUES(?,?,'ARGON2ID_V1',?,?)`,
		plan.UserID,
		plan.PasswordHash,
		plan.Now,
		plan.Now,
	); err != nil {
		return fmt.Errorf("create initial credential: %w", err)
	}
	result, err := recordstore.UpdateInstanceState(ctx, records.executor, recordstore.Update{
		Set: `state='COMPLETED',bootstrap_kind=?,initial_admin_user_id=?,test_default_password_active=?,
 version=version+1,updated_at_ms=?,initialized_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=1 AND state='PENDING'`,
		}, Values: []any{
			plan.Kind,
			plan.UserID,
			plan.TestDefault,
			plan.Now,
			plan.Now,
		},
	})
	if err := authChanged(result, err, accounts.ErrInitializationDone); err != nil {
		return err
	}
	if err := authRecords(records).insertSession(ctx, plan.Session); err != nil {
		return err
	}
	_, err = records.executor.ExecContext(
		ctx,
		`INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms)
 VALUES(?,'SYSTEM',NULL,?,'INSTANCE_INITIALIZED','USER',?,NULL,'{}','{}',NULL,?)`,
		plan.AuditID,
		plan.ActorLabel,
		plan.UserID,
		plan.Now,
	)
	if err != nil {
		return fmt.Errorf("audit initialization: %w", err)
	}
	return nil
}
