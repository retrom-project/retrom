package recordstore

import (
	"context"
	"database/sql"
	"fmt"
)

func deleteRows(
	ctx context.Context, db DBTX, table string, scope Scope,
) (sql.Result, error) {
	if scope.Where == "" {
		return nil, fmt.Errorf("%w: missing delete scope", ErrInvariant)
	}
	result, err := db.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+scope.Where, scope.Args...)
	if err != nil {
		return nil, fmt.Errorf("delete %s: %w", table, err)
	}
	return result, nil
}

func DeleteGameAssets(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "game_assets", scope)
}

func DeleteGameFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "game_files", scope)
}

func DeleteReviewPreviewSessions(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "review_preview_sessions", scope)
}

func DeleteReviewRuntimeScreenshots(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "review_runtime_screenshots", scope)
}

func DeleteSaveStates(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "save_states", scope)
}

func DeleteScrapeCandidateAssets(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "scrape_candidate_assets", scope)
}

func DeleteVariantDependencies(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "variant_dependencies", scope)
}

func DeleteVariantFiles(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "variant_files", scope)
}
