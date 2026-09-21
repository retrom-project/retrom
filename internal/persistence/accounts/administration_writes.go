package accounts

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/accounts"
)

func (records administrationRecords) Update(ctx context.Context, plan accounts.AdministrationUpdate) error {
	change := plan.Change
	before := plan.Before
	sessionIncrement := 0
	if change.Security.Sessions {
		sessionIncrement = 1
	}
	result, err := recordstore.UpdateUsers(ctx, records.executor, recordstore.Update{
		Set: `role=?,status=?,session_version=session_version+?,version=version+1,updated_at_ms=?,
 disabled_at_ms=CASE WHEN ?='DISABLED' THEN COALESCE(disabled_at_ms,?) ELSE NULL END`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND status!='DELETED'`,
			Args: []any{
				before.User.UserID,
				before.User.Version,
			},
		},
		Values: []any{change.Role, change.Status, sessionIncrement, plan.Now, change.Status, plan.Now},
	})
	if err := authChanged(result, err, accounts.ErrUserVersion); err != nil {
		return err
	}
	return records.revokeSecurity(ctx, before, change.Security, plan.Now)
}

func (records administrationRecords) Delete(ctx context.Context, plan accounts.AdministrationDeletion) error {
	before := plan.Before
	result, err := recordstore.UpdateUsers(ctx, records.executor, recordstore.Update{
		Set: `status='DELETED',session_version=session_version+1,version=version+1,
 updated_at_ms=?,disabled_at_ms=NULL,deleted_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND status!='DELETED'`,
			Args: []any{
				before.User.UserID,
				before.User.Version,
			},
		}, Values: []any{
			plan.Now,
			plan.Now,
		},
	})
	if err := authChanged(result, err, accounts.ErrUserVersion); err != nil {
		return err
	}
	if err := records.revokeSecurity(ctx, before, plan.Security, plan.Now); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`DELETE FROM user_credentials WHERE user_id=?`,
		before.User.UserID,
	); err != nil {
		return fmt.Errorf("delete user credential: %w", err)
	}
	if plan.ClearTestDefault {
		if err := (authRecords{records.executor}).clearDefaultCredential(ctx, plan.Now); err != nil {
			return err
		}
	}
	return nil
}
