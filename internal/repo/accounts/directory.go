package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	"retrom/internal/service/accounts"
)

type DirectoryRepository struct{ database *sql.DB }

func NewDirectory(database *sql.DB) *DirectoryRepository { return &DirectoryRepository{database} }

const adminUserProjection = `SELECT u.id,u.username,u.display_name,u.role,u.status,u.version,
 u.created_at_ms,u.last_login_at_ms,
 (SELECT count(*) FROM auth_sessions s WHERE s.user_id=u.id AND s.revoked_at_ms IS NULL
 AND s.user_session_version=u.session_version
 AND s.idle_expires_at_ms>? AND s.absolute_expires_at_ms>? AND u.status='ENABLED') FROM users u`

func (repository *DirectoryRepository) Get(
	ctx context.Context,
	id string,
	now int64,
) (accounts.AdminUser, bool, error) {
	user, err := scanAdminUser(repository.database.QueryRowContext(ctx, adminUserProjection+` WHERE u.id=?`, now, now, id))
	if errors.Is(err, sql.ErrNoRows) {
		return user, false, nil
	}
	if err != nil {
		return user, false, fmt.Errorf("query admin user: %w", err)
	}
	return user, true, nil
}

func (repository *DirectoryRepository) List(
	ctx context.Context,
	query accounts.UserQuery,
) ([]accounts.AdminUser, error) {
	builder := directorySQL{query: adminUserProjection + ` WHERE 1=1`, arguments: []any{query.Now, query.Now}}
	if query.Text != "" {
		builder.add(` AND (instr(u.username,lower(?))>0 OR instr(u.display_name,?)>0)`, query.Text, query.Text)
	}
	if query.Role != "" {
		builder.add(` AND u.role=?`, query.Role)
	}
	builder.status(query.Status)
	builder.sort(query)
	builder.add(` LIMIT ?`, query.Limit)
	rows, err := repository.database.QueryContext(ctx, builder.query, builder.arguments...)
	if err != nil {
		return nil, fmt.Errorf("query user directory: %w", err)
	}
	defer func() { cleanup.Error("close user directory", rows.Close()) }()
	users := make([]accounts.AdminUser, 0)
	for rows.Next() {
		user, err := scanAdminUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user directory: %w", err)
	}
	return users, nil
}

func scanAdminUser(scanner dbexec.Scanner) (accounts.AdminUser, error) {
	var user accounts.AdminUser
	err := scanner.Scan(
		&user.UserID,
		&user.Username,
		&user.DisplayName,
		&user.Role,
		&user.Status,
		&user.Version,
		&user.CreatedAtMS,
		&user.LastLoginAtMS,
		&user.ActiveSessionCount,
	)
	if err != nil {
		return user, fmt.Errorf("scan user directory entry: %w", err)
	}
	return user, nil
}
