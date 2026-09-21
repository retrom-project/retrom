package accounts

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	"retrom/internal/service/accounts"
)

func (records administrationRecords) revokeSecurity(
	ctx context.Context,
	before accounts.ManagedUser,
	security accounts.UserSecurity,
	now int64,
) error {
	if security.Sessions {
		_, err := records.executor.ExecContext(
			ctx,
			`UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason=? WHERE user_id=? AND revoked_at_ms IS NULL`,
			now,
			security.Reason,
			before.User.UserID,
		)
		if err != nil {
			return fmt.Errorf("revoke managed user sessions: %w", err)
		}
	}
	if security.CreatedLinks {
		if err := records.revokeCreatedLinks(ctx, before.User.UserID, now); err != nil {
			return err
		}
	}
	if security.TargetLinks {
		if err := records.revokeTargetLinks(ctx, before.User.UserID, now); err != nil {
			return err
		}
	}
	if security.Launches {
		_, err := sessionstore.ChangeLaunch(
			ctx,
			records.executor,
			recordstore.Update{
				Set: `state='REVOKED',finished_at_ms=?,version=version+1,updated_at_ms=?`,
				Scope: recordstore.Scope{
					Where: `profile_id=? AND state IN ('CREATED','ACTIVE')`,
					Args: []any{
						before.ProfileID,
					},
				},
				Values: []any{
					now,
					now,
				},
			},
		)
		if err != nil {
			return fmt.Errorf("revoke managed user launches: %w", err)
		}
	}
	return nil
}

func (records administrationRecords) revokeCreatedLinks(ctx context.Context, userID string, now int64) error {
	_, err := recordstore.UpdateAccountLinks(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1`,
			Scope: recordstore.Scope{
				Where: `created_by_user_id=? AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?`,
				Args: []any{
					userID,
					now,
				},
			},
			Values: []any{
				now,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("revoke managed user-created links: %w", err)
	}
	return nil
}

func (records administrationRecords) revokeTargetLinks(ctx context.Context, userID string, now int64) error {
	_, err := recordstore.UpdateAccountLinks(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1`,
			Scope: recordstore.Scope{
				Where: `kind='PASSWORD_RESET' AND target_user_id=? AND consumed_at_ms IS NULL
 AND revoked_at_ms IS NULL AND expires_at_ms>?`,
				Args: []any{userID, now},
			},
			Values: []any{now},
		},
	)
	if err != nil {
		return fmt.Errorf("revoke managed user-targeted links: %w", err)
	}
	return nil
}
