package accounts

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/accounts"
)

func (records authRecords) Login(
	ctx context.Context,
	credential accounts.LoginCredential,
	value accounts.SessionRecord,
) error {
	result, err := recordstore.UpdateUsers(ctx, records.executor, recordstore.Update{
		Set: `last_login_at_ms=?,updated_at_ms=?`, Scope: recordstore.Scope{
			Where: `id=? AND status='ENABLED' AND session_version=?`,
			Args: []any{
				credential.User.UserID,
				credential.SessionVersion,
			},
		}, Values: []any{
			value.CreatedAt,
			value.CreatedAt,
		},
	})
	if err := authChanged(result, err, accounts.ErrAuthentication); err != nil {
		return err
	}
	_, err = records.executor.ExecContext(
		ctx,
		`INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,created_at_ms,last_seen_at_ms,
 idle_expires_at_ms,absolute_expires_at_ms)
 VALUES(?,?,?,?,?,?,?,?)`,
		value.ID,
		value.UserID,
		value.Hash[:],
		value.SessionVersion,
		value.CreatedAt,
		value.LastSeen,
		value.IdleExpiry,
		value.AbsoluteExpiry,
	)
	if err != nil {
		return fmt.Errorf("insert login session: %w", err)
	}
	return nil
}

func (records authRecords) Refresh(ctx context.Context, value accounts.SessionRefresh) error {
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE auth_sessions SET last_seen_at_ms=?,idle_expires_at_ms=?
 WHERE id=? AND revoked_at_ms IS NULL AND last_seen_at_ms=?`,
		value.LastSeen,
		value.IdleExpiry,
		value.ID,
		value.ExpectedLastSeen,
	)
	return authChanged(result, err, accounts.ErrAuthenticationNeeded)
}

func (records authRecords) Revoke(ctx context.Context, id string, now int64) error {
	_, err := records.executor.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='LOGOUT'
 WHERE id=? AND revoked_at_ms IS NULL`, now, id)
	if err != nil {
		return fmt.Errorf("revoke login session: %w", err)
	}
	return nil
}

func authChanged(result sql.Result, err, conflict error) error {
	if err != nil {
		return fmt.Errorf("update authentication state: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count authentication state: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("authentication state conflict: %w", conflict)
	}
	return nil
}
