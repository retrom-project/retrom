package recordstore

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
)

func deleteRows(
	ctx context.Context, db dbapi.Executor, table string, scope Scope,
) (sql.Result, error) {
	if scope.Where == "" {
		return nil, fmt.Errorf("%w: missing delete scope", ErrInvariant)
	}
	return Atomic(ctx, db, func(tx dbapi.Executor) (sql.Result, error) {
		return tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+scope.Where, scope.Args...)
	})
}

func DeleteGameAssets(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "game_assets", scope)
}

func DeleteGameFiles(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "game_files", scope)
}

func DeleteReviewPreviewSessions(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "review_preview_sessions", scope)
}

func DeleteReviewRuntimeScreenshots(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "review_runtime_screenshots", scope)
}

func DeleteSaveStates(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "save_states", scope)
}

func DeleteScrapeCandidateAssets(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "scrape_candidate_assets", scope)
}

func DeleteVariantDependencies(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "variant_dependencies", scope)
}

func DeleteVariantFiles(
	ctx context.Context, db dbapi.Executor, scope Scope,
) (sql.Result, error) {
	return deleteRows(ctx, db, "variant_files", scope)
}

func DeleteImportItemAssets(ctx context.Context, db dbapi.Executor, scope Scope) (sql.Result, error) {
	return deleteRows(ctx, db, "import_item_assets", scope)
}
