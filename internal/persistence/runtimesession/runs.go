package runtimesession

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
)

func (r *Repository) Runs(ctx context.Context, profile, id string, now int64) ([]string, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT id FROM (
 SELECT l.id,l.created_at_ms FROM launch_sessions l JOIN games g ON g.id=l.game_id
 WHERE l.profile_id=? AND (?='' OR l.id=?) AND l.state IN ('CREATED','ACTIVE')
 AND l.hard_expires_at_ms>? AND (l.state='ACTIVE' OR l.bootstrap_expires_at_ms>?) AND g.deleted_at_ms IS NULL
 UNION ALL
 SELECT p.id,p.created_at_ms FROM runtime_preview_sessions p JOIN users u ON u.id=p.actor_user_id
 WHERE u.profile_id=? AND (?='' OR p.id=?) AND p.state IN ('CREATED','ACTIVE')
 AND p.hard_expires_at_ms>? AND (p.state='ACTIVE' OR p.bootstrap_expires_at_ms>?)) ORDER BY created_at_ms DESC,id DESC`,
		profile, id, id, now, now, profile, id, id, now, now)
	if err != nil {
		return nil, fmt.Errorf("read owned runtime runs: %w", err)
	}
	defer func() { cleanup.Error("close runtime sessions", rows.Close()) }()
	result := []string{}
	for rows.Next() {
		var run string
		if err = rows.Scan(&run); err != nil {
			return nil, fmt.Errorf("scan runtime run: %w", err)
		}
		result = append(result, run)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime runs: %w", err)
	}
	return result, nil
}

func (r *Repository) ExtendRun(ctx context.Context, profile, id string, now, expires int64) error {
	var needsRenewal bool
	err := dbapi.QueryRowContext(ctx, r.database, `SELECT EXISTS(SELECT 1 FROM launch_sessions
 WHERE id=? AND profile_id=? AND state='ACTIVE' AND hard_expires_at_ms>? AND hard_expires_at_ms<?)`,
		id, profile, now, expires).Scan(&needsRenewal)
	if err != nil {
		return fmt.Errorf("inspect runtime run renewal: %w", err)
	}
	if !needsRenewal {
		return nil
	}
	err = dbapi.RetryTransaction(ctx, r.database, func(tx dbapi.Tx) error {
		_, err := sessionstore.ChangeLaunch(ctx, tx, recordstore.Update{
			Set: `hard_expires_at_ms=?,updated_at_ms=?`, Values: []any{expires, now},
			Scope: recordstore.Scope{
				Where: `id=? AND profile_id=? AND state='ACTIVE' AND hard_expires_at_ms>? AND hard_expires_at_ms<?`,
				Args:  []any{id, profile, now, expires},
			},
		})
		if err != nil {
			return fmt.Errorf("renew runtime run: %w", err)
		}
		_, err = recordstore.UpdateIsolatedRuntimeCapabilities(ctx, tx, recordstore.Update{
			Set: `expires_at_ms=?`, Values: []any{expires},
			Scope: recordstore.Scope{Where: `launch_id=? AND profile_id=? AND revoked_at_ms IS NULL AND expires_at_ms>?
	 AND expires_at_ms<? AND EXISTS(SELECT 1 FROM launch_sessions l WHERE l.id=launch_id AND l.state='ACTIVE'
	 AND l.hard_expires_at_ms>=?)`, Args: []any{id, profile, now, expires, expires}},
		})
		if err != nil {
			return fmt.Errorf("renew isolated runtime grant: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit runtime run renewal: %w", err)
	}
	return nil
}

func (r *Repository) FinishablePreview(ctx context.Context, profile, id string, now int64) (bool, error) {
	var valid bool
	err := dbapi.QueryRowContext(ctx, r.database, `SELECT EXISTS(SELECT 1 FROM runtime_preview_sessions p
 JOIN users u ON u.id=p.actor_user_id WHERE p.id=? AND u.profile_id=? AND p.state IN ('CREATED','ACTIVE','FINISHED')
 AND p.hard_expires_at_ms>?)`, id, profile, now).Scan(&valid)
	if err != nil {
		return false, fmt.Errorf("read preview finish authority: %w", err)
	}
	return valid, nil
}
