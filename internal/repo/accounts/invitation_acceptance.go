package accounts

import (
	"context"
	"fmt"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/recordstore"
)

func (records linkRecords) Accept(ctx context.Context, plan accounts.InvitationAcceptance) error {
	if _, err := recordstore.CreateProfiles(
		ctx,
		records.executor,
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`,
		plan.ProfileID,
		plan.User.DisplayName,
		plan.Now,
	); err != nil {
		return fmt.Errorf("create invited profile: %w", err)
	}
	result, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,'ENABLED',?,?) ON CONFLICT(username) DO NOTHING`,
		plan.User.UserID,
		plan.ProfileID,
		plan.User.Username,
		plan.User.DisplayName,
		plan.User.Role,
		plan.Now,
		plan.Now,
	)
	if err := authChanged(result, err, accounts.ErrUsernameUnavailable); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES(?,?,'ARGON2ID_V1',?,?)`,
		plan.User.UserID,
		plan.PasswordHash,
		plan.Now,
		plan.Now,
	); err != nil {
		return fmt.Errorf("create invited credential: %w", err)
	}
	result, err = recordstore.UpdateAccountLinks(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `consumed_at_ms=?,consumed_by_user_id=?,version=version+1`,
			Scope: recordstore.Scope{
				Where: `id=? AND kind='INVITATION' AND version=?
AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?`,
				Args: []any{
					plan.LinkID,
					plan.LinkVersion,
					plan.Now,
				},
			},
			Values: []any{
				plan.Now,
				plan.User.UserID,
			},
		},
	)
	if err := authChanged(result, err, accounts.ErrAccountLinkUnavailable); err != nil {
		return err
	}
	return (authRecords{records.executor}).insertSession(ctx, plan.Session)
}
