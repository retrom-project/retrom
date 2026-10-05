package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/accounts"
)

type (
	PasswordRepository struct{ reader, writer dbapi.DB }
	passwordRecords    struct{ executor dbapi.Executor }
)

func NewPasswords(reader, writer dbapi.DB) *PasswordRepository {
	return &PasswordRepository{reader: reader, writer: writer}
}

func (repository *PasswordRepository) Current(
	ctx context.Context,
	actor accounts.PasswordActor,
	now int64,
) (accounts.PasswordState, bool, error) {
	return (passwordRecords{repository.reader}).Current(ctx, actor, now)
}

func (repository *PasswordRepository) WithWrite(ctx context.Context, work func(accounts.PasswordScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.writer, func(tx dbapi.Tx) error {
		records := passwordRecords{tx}
		if err := work(accounts.PasswordScope{Read: records, Write: records}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit password rotation: %w", err)
	}
	return nil
}

func (records passwordRecords) Current(
	ctx context.Context,
	actor accounts.PasswordActor,
	now int64,
) (accounts.PasswordState, bool, error) {
	var state accounts.PasswordState
	value := &state.Credential
	err := dbapi.QueryRowContext(
		ctx, records.executor,

		`SELECT u.id,u.profile_id,u.username,u.display_name,u.role,u.status,u.session_version,c.password_hash,
 EXISTS(SELECT 1 FROM auth_sessions s WHERE s.id=? AND s.user_id=u.id AND s.user_session_version=u.session_version
 AND s.revoked_at_ms IS NULL AND s.idle_expires_at_ms>? AND s.absolute_expires_at_ms>?)
 FROM users u JOIN user_credentials c ON c.user_id=u.id WHERE u.id=?`,
		actor.SessionID,
		now,
		now,
		actor.UserID,
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
			&state.SessionCurrent,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	if err != nil {
		return state, false, fmt.Errorf("query password security state: %w", err)
	}
	return state, true, nil
}
