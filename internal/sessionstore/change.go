package sessionstore

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/recordstore"
)

func ChangeLaunch(ctx context.Context, db recordstore.DBTX, change recordstore.Update) (sql.Result, error) {
	return changeSession(
		ctx,
		db,
		change,
		"launch_sessions",
		recordstore.UpdateLaunchSessions,
		updateLaunchRelations,
	)
}

func ChangePreview(ctx context.Context, db recordstore.DBTX, change recordstore.Update) (sql.Result, error) {
	return changeSession(
		ctx,
		db,
		change,
		"review_preview_sessions",
		recordstore.UpdateReviewPreviewSessions,
		revokePreviewCapability,
	)
}

type (
	sessionUpdate    func(context.Context, recordstore.DBTX, recordstore.Update) (sql.Result, error)
	sessionRelations func(context.Context, recordstore.DBTX, string) error
)

func changeSession(
	ctx context.Context,
	db recordstore.DBTX,
	change recordstore.Update,
	table string,
	update sessionUpdate,
	apply sessionRelations,
) (sql.Result, error) {
	result, err := recordstore.Atomic(ctx, db, func(tx recordstore.DBTX) (sql.Result, error) {
		ids, err := sessionIDs(ctx, tx, table, change.Scope)
		if err != nil {
			return nil, err
		}
		result, err := update(ctx, tx, change)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if err := apply(ctx, tx, id); err != nil {
				return nil, err
			}
		}
		return result, nil
	})
	if err != nil {
		return nil, fmt.Errorf("change session and relations: %w", err)
	}
	return result, nil
}

func sessionIDs(ctx context.Context, db recordstore.DBTX, table string, scope recordstore.Scope) ([]string, error) {
	if scope.Where == "" {
		return nil, fmt.Errorf("%w: missing session scope", recordstore.ErrInvariant)
	}
	rows, err := db.QueryContext(ctx, "SELECT id FROM "+table+" WHERE "+scope.Where, scope.Args...)
	if err != nil {
		return nil, fmt.Errorf("select sessions: %w", err)
	}
	defer func() { cleanup.Error("close selected sessions", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read session identity: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session identities: %w", err)
	}
	return ids, nil
}
