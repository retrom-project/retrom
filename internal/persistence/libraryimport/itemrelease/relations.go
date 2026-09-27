package itemrelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

func Relations(ctx context.Context, executor dbapi.Executor, facts *application.EffectOwner) error {
	err := dbapi.QueryRowContext(ctx, executor, `SELECT import_job_id FROM import_items WHERE id=?`,
		facts.Owner.Scope.ID).Scan(&facts.ParentID)
	if err != nil {
		return fmt.Errorf("read import item release relations: %w", err)
	}
	return nil
}

func JobLinks(ctx context.Context, executor dbapi.Executor, id string) ([]application.Scope, error) {
	ids, err := dbapi.QueryStrings(ctx, executor, `SELECT id FROM import_items WHERE import_job_id=? ORDER BY id`, id)
	if err != nil {
		return nil, wrapErr(err)
	}
	links := make([]application.Scope, 0, len(ids))
	for _, id := range ids {
		links = append(links, application.Scope{Type: application.ScopeImportItem, ID: id})
	}
	return links, nil
}
