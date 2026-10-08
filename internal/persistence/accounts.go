package persistence

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"retrom/internal/model"
)

const userJSON = `jsonb_build_object('id',u.id,'username',u.username,'displayName',u.display_name,
 'role',u.role,'status',u.status,'version',u.version,'createdAtMs',u.created_at_ms,'lastLoginAtMs',u.last_login_at_ms)`

func (r *Repository) Initialized(ctx context.Context) (bool, error) {
	var initialized bool
	query := "SELECT EXISTS(SELECT 1 FROM instance_state_tab WHERE state='initialized')"
	err := r.db.QueryRow(ctx, query).Scan(&initialized)
	if err != nil {
		return false, failure("read instance state", err)
	}
	return initialized, nil
}

func (r *Repository) CreateUser(ctx context.Context, user model.User, hash string, now int64) error {
	err := r.Execute(ctx, `INSERT INTO user_tab
 (id,username,display_name,role,status,session_version,version,created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,$4,'active',1,1,$5,$5)`, user.ID, user.Username, user.DisplayName, user.Role, now)
	if err != nil {
		return err
	}
	return r.Execute(ctx, `INSERT INTO user_credential_tab
 (user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms) VALUES($1,$2,'ARGON2ID_V1',$3,$3)`,
		user.ID, hash, now)
}

func (r *Repository) Initialize(ctx context.Context, user model.User, hash string, now int64) error {
	if err := r.Execute(ctx, `INSERT INTO instance_state_tab
 (id,state,bootstrap_kind,initial_admin_user_id,test_default_password_active,version,created_at_ms,
 updated_at_ms,initialized_at_ms) VALUES('instance','initialized','manual',$1,false,1,$2,$2,$2)`,
		user.ID, now); err != nil {
		return err
	}
	return r.CreateUser(ctx, user, hash, now)
}

func (r *Repository) Credential(ctx context.Context, username string) (model.User, string, error) {
	user, err := readJSON[model.User](ctx, r, "SELECT "+userJSON+" FROM user_tab u WHERE username=$1", username)
	if err != nil {
		return user, "", err
	}
	var hash string
	err = r.db.QueryRow(ctx, "SELECT password_hash FROM user_credential_tab WHERE user_id=$1", user.ID).Scan(&hash)
	if err != nil {
		return user, "", failure("read credential", err)
	}
	return user, hash, nil
}

func (r *Repository) CreateSession(ctx context.Context, id, userID, token string, now int64) error {
	digest := sha256.Sum256([]byte(token))
	tag, err := r.db.Exec(ctx, `INSERT INTO auth_session_tab
 (id,user_id,token_sha256,user_session_version,created_at_ms,last_seen_at_ms,idle_expires_at_ms,absolute_expires_at_ms)
 SELECT $1,id,$3,session_version,$4::bigint,$4::bigint,$4::bigint+28800000,$4::bigint+86400000
 FROM user_tab WHERE id=$2 AND status='active'`,
		id, userID, digest[:], now)
	if err != nil {
		return failure("create session", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrUnauthorized
	}
	return nil
}

// FenceCredential runs after password verification, inside the short session/password transaction.
// Lock order matches ChangePassword: credential first, then user.
func (r *Repository) FenceCredential(ctx context.Context, userID, expected string) error {
	var current string
	if err := r.db.QueryRow(ctx, "SELECT password_hash FROM user_credential_tab WHERE user_id=$1 FOR UPDATE",
		userID).Scan(&current); err != nil {
		return failure("lock verified credential", err)
	}
	if current != expected {
		return model.ErrUnauthorized
	}
	var status string
	if err := r.db.QueryRow(ctx, "SELECT status FROM user_tab WHERE id=$1 FOR UPDATE", userID).Scan(&status); err != nil {
		return failure("lock verified account", err)
	}
	if status != "active" {
		return model.ErrUnauthorized
	}
	return nil
}

func (r *Repository) Authenticate(ctx context.Context, token string, now int64) (model.Principal, error) {
	digest := sha256.Sum256([]byte(token))
	principal := model.Principal{}
	query := "SELECT " + userJSON + ` FROM user_tab u JOIN auth_session_tab s ON s.user_id=u.id
 WHERE s.token_sha256=$1 AND u.status='active' AND s.revoked_at_ms IS NULL AND
 s.user_session_version=u.session_version AND s.idle_expires_at_ms>$2 AND s.absolute_expires_at_ms>$2`
	user, err := readJSON[model.User](ctx, r, query, digest[:], now)
	if errors.Is(err, model.ErrNotFound) {
		return principal, model.ErrUnauthorized
	}
	if err != nil {
		return principal, err
	}
	var id string
	if err = r.db.QueryRow(ctx, "SELECT id FROM auth_session_tab WHERE token_sha256=$1", digest[:]).Scan(&id); err != nil {
		return principal, failure("read session", err)
	}
	if err = r.Execute(ctx, `UPDATE auth_session_tab SET last_seen_at_ms=$2,
 idle_expires_at_ms=LEAST($2::bigint+28800000,absolute_expires_at_ms) WHERE id=$1`, id, now); err != nil {
		return principal, err
	}
	principal.User = user
	principal.SessionID = id
	return principal, nil
}

func (r *Repository) Logout(ctx context.Context, sessionID string, now int64) error {
	query := "UPDATE auth_session_tab SET revoked_at_ms=$2,revoked_reason='logout' WHERE id=$1"
	return r.Execute(ctx, query, sessionID, now)
}

func (r *Repository) LoginTime(ctx context.Context, id string, now int64) error {
	return r.Execute(ctx, "UPDATE user_tab SET last_login_at_ms=$2 WHERE id=$1", id, now)
}

func (r *Repository) User(ctx context.Context, id string) (model.User, error) {
	return readJSON[model.User](ctx, r, "SELECT "+userJSON+" FROM user_tab u WHERE id=$1", id)
}

func (r *Repository) Users(ctx context.Context, q model.Query) (model.Page[model.User], error) {
	page := model.Page[model.User]{Offset: q.Offset, Limit: q.Limit}
	items, err := listJSON[model.User](ctx, r, "SELECT "+userJSON+` FROM user_tab u
 WHERE username ILIKE '%'||$1||'%' OR display_name ILIKE '%'||$1||'%' ORDER BY created_at_ms DESC,id
 LIMIT $2 OFFSET $3`, q.Search, q.Limit, q.Offset)
	if err != nil {
		return page, err
	}
	count, err := r.Count(ctx, `SELECT count(*) FROM user_tab WHERE username ILIKE '%'||$1||'%'
 OR display_name ILIKE '%'||$1||'%'`, q.Search)
	page.Items = items
	page.Total = count
	return page, err
}

func (r *Repository) ChangePassword(ctx context.Context, id, hash string, now int64) error {
	if err := r.Execute(ctx, `UPDATE user_credential_tab SET password_hash=$2,password_changed_at_ms=$3 WHERE user_id=$1`,
		id, hash, now); err != nil {
		return err
	}
	query := "UPDATE user_tab SET session_version=session_version+1,version=version+1 WHERE id=$1"
	if err := r.Execute(ctx, query, id); err != nil {
		return err
	}
	return r.Execute(ctx, `UPDATE auth_session_tab SET revoked_at_ms=$2,revoked_reason='password_changed'
 WHERE user_id=$1 AND revoked_at_ms IS NULL`, id, now)
}

func (r *Repository) UpdateUser(ctx context.Context, user model.User, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE user_tab SET display_name=$2,role=$3,status=$4,version=version+1,
 session_version=session_version+1,updated_at_ms=$5 WHERE id=$1 AND version=$6`,
		user.ID, user.DisplayName, user.Role, user.Status, now, user.Version)
	if err != nil {
		return failure("update user", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	if user.Status == "active" {
		return nil
	}
	if err = r.Execute(ctx, `UPDATE auth_session_tab SET revoked_at_ms=$2,revoked_reason='account_disabled'
 WHERE user_id=$1 AND revoked_at_ms IS NULL`, user.ID, now); err != nil {
		return err
	}
	return r.Execute(ctx,
		`UPDATE account_link_tab SET revoked_at_ms=$2,version=version+1,revoked_by_kind='account_disabled'
 WHERE target_user_id=$1 AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL`, user.ID, now)
}

func (r *Repository) RequireUser(ctx context.Context, id string) error {
	user, err := r.User(ctx, id)
	if err != nil {
		return err
	}
	if user.Status == "deleted" {
		return fmt.Errorf("account deleted: %w", model.ErrNotFound)
	}
	return nil
}
