package runtimesession

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/runtimesession"
)

type Repository struct{ database dbapi.DB }

func New(database dbapi.DB) *Repository { return &Repository{database: database} }

const sessionSelect = `SELECT r.id,r.auth_session_id,r.token_sha256,r.created_at_ms,r.renewed_at_ms,r.expires_at_ms,
 u.id,u.profile_id,u.status='ENABLED',a.revoked_at_ms IS NOT NULL,u.session_version,a.user_session_version
 FROM runtime_sessions r JOIN auth_sessions a ON a.id=r.auth_session_id JOIN users u ON u.id=a.user_id `

func scanSession(row dbapi.Scanner) (runtimesession.Snapshot, bool, error) {
	var value runtimesession.Snapshot
	var hash []byte
	s := &value.Session
	err := row.Scan(&s.ID, &s.AuthSessionID, &hash, &s.CreatedAtMS, &s.RenewedAtMS, &s.ExpiresAtMS,
		&s.UserID, &s.ProfileID, &value.Enabled, &value.Revoked, &value.UserVersion, &value.SessionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return value, false, nil
	}
	if err != nil {
		return value, false, fmt.Errorf("read shared runtime session: %w", err)
	}
	copy(s.Hash[:], hash)
	return value, true, nil
}

func (r *Repository) Find(ctx context.Context, hash [32]byte) (runtimesession.Snapshot, bool, error) {
	return scanSession(dbapi.QueryRowContext(ctx, r.database, sessionSelect+`WHERE r.token_sha256=?`, hash[:]))
}

func (r *Repository) Ensure(
	ctx context.Context, authID string, candidate runtimesession.Session, now int64,
) (runtimesession.Snapshot, error) {
	return r.writeSession(ctx, "runtime issuance", func(tx dbapi.Tx) (runtimesession.Snapshot, error) {
		var valid bool
		err := dbapi.QueryRowContext(ctx, tx, `SELECT EXISTS(SELECT 1 FROM auth_sessions a JOIN users u ON u.id=a.user_id
 WHERE a.id=? AND a.revoked_at_ms IS NULL AND a.idle_expires_at_ms>? AND a.absolute_expires_at_ms>?
 AND u.status='ENABLED' AND u.session_version=a.user_session_version)`, authID, now, now).Scan(&valid)
		if err != nil {
			return runtimesession.Snapshot{}, fmt.Errorf("authorize runtime issuance: %w", err)
		}
		if !valid {
			return runtimesession.Snapshot{}, runtimesession.ErrCredential
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO runtime_sessions(
 id,auth_session_id,token_sha256,created_at_ms,renewed_at_ms,expires_at_ms)
 VALUES(?,?,?,?,?,?) ON CONFLICT(auth_session_id) DO UPDATE SET id=excluded.id,token_sha256=excluded.token_sha256,
 created_at_ms=excluded.created_at_ms,renewed_at_ms=excluded.renewed_at_ms,expires_at_ms=excluded.expires_at_ms
 WHERE runtime_sessions.expires_at_ms<=?`,
			candidate.ID, authID, candidate.Hash[:], now, now, candidate.ExpiresAtMS, now)
		if err != nil {
			return runtimesession.Snapshot{}, fmt.Errorf("store shared runtime session: %w", err)
		}
		value, found, err := scanSession(dbapi.QueryRowContext(ctx, tx, sessionSelect+`WHERE r.auth_session_id=?`, authID))
		if err != nil {
			return value, err
		}
		if !found {
			return value, runtimesession.ErrCredential
		}
		return value, nil
	})
}

func (r *Repository) Renew(ctx context.Context, id string, now, expires int64) (runtimesession.Snapshot, error) {
	return r.writeSession(ctx, "runtime renewal", func(tx dbapi.Tx) (runtimesession.Snapshot, error) {
		_, err := tx.ExecContext(ctx, `UPDATE runtime_sessions SET renewed_at_ms=?,expires_at_ms=?
 WHERE id=? AND expires_at_ms>? AND renewed_at_ms<? AND EXISTS(
 SELECT 1 FROM auth_sessions a JOIN users u ON u.id=a.user_id WHERE a.id=runtime_sessions.auth_session_id
 AND a.revoked_at_ms IS NULL AND u.status='ENABLED' AND u.session_version=a.user_session_version)`,
			now, expires, id, now, now-runtimesession.RenewalAgeMS)
		if err != nil {
			return runtimesession.Snapshot{}, fmt.Errorf("update runtime expiry: %w", err)
		}
		value, found, err := scanSession(dbapi.QueryRowContext(ctx, tx, sessionSelect+`WHERE r.id=?`, id))
		if err != nil {
			return value, err
		}
		if !found {
			return value, runtimesession.ErrCredential
		}
		return value, nil
	})
}

// writeSession rechecks account/session authority on each database-only attempt.
func (r *Repository) writeSession(ctx context.Context, operation string,
	work func(dbapi.Tx) (runtimesession.Snapshot, error),
) (runtimesession.Snapshot, error) {
	var value runtimesession.Snapshot
	err := dbapi.RetryTransaction(ctx, r.database, func(tx dbapi.Tx) error {
		var err error
		value, err = work(tx)
		return err
	})
	if err != nil {
		return runtimesession.Snapshot{}, fmt.Errorf("commit %s: %w", operation, err)
	}
	return value, nil
}
