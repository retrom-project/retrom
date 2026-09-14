package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/accounts"
)

func (records authRecords) Credential(ctx context.Context, username string) (accounts.LoginCredential, bool, error) {
	var value accounts.LoginCredential
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT u.id,u.profile_id,u.username,u.display_name,u.role,u.status,u.session_version,c.password_hash
 FROM users u JOIN user_credentials c ON c.user_id=u.id WHERE u.username=?`,
		username,
	).
		Scan(
			&value.User.UserID,
			&value.ProfileID,
			&value.User.Username,
			&value.User.DisplayName,
			&value.User.Role,
			&value.Status,
			&value.SessionVersion,
			&value.PasswordHash,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return value, false, nil
	}
	if err != nil {
		return value, false, fmt.Errorf("query login credential: %w", err)
	}
	return value, true, nil
}

func (records authRecords) Session(ctx context.Context, digest [32]byte) (accounts.SessionSnapshot, bool, error) {
	var value accounts.SessionSnapshot
	err := records.executor.QueryRowContext(
		ctx,
		`SELECT s.id,u.id,u.profile_id,u.username,u.display_name,u.role,u.status,u.session_version,
 s.user_session_version,s.last_seen_at_ms,s.idle_expires_at_ms,s.absolute_expires_at_ms,s.revoked_at_ms
 FROM auth_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_sha256=?`,
		digest[:],
	).Scan(
		&value.ID,
		&value.User.UserID,
		&value.ProfileID,

		&value.User.Username,
		&value.User.DisplayName,
		&value.User.Role,
		&value.Status,
		&value.UserVersion,
		&value.SessionVersion,
		&value.LastSeen,
		&value.IdleExpiry,
		&value.AbsoluteExpiry,
		&value.RevokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return value, false, nil
	}
	if err != nil {
		return value, false, fmt.Errorf("query authentication session: %w", err)
	}
	return value, true, nil
}
