package sourcerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

func Relations(ctx context.Context, executor dbapi.Executor, facts *application.EffectOwner) error {
	err := dbapi.QueryRowContext(ctx, executor, `SELECT import_id,COALESCE(existing_game_id,''),EXISTS(
 SELECT 1 FROM import_items item JOIN import_item_duplicate_matches duplicate ON duplicate.import_item_id=item.id
 WHERE item.id=source.library_import_item_id AND item.state='DISCARDED'
 AND duplicate.existing_game_id=source.existing_game_id)
 FROM source_import_items source WHERE source.id=?`, facts.Owner.Scope.ID).Scan(&facts.ParentID,
		&facts.ExistingGameID, &facts.DuplicateMatch)
	if err != nil {
		return fmt.Errorf("read source release relations: %w", err)
	}
	return nil
}

func BoundSources(ctx context.Context, executor dbapi.Executor, id string) ([]application.Scope, error) {
	ids, err := dbapi.QueryStrings(ctx, executor,
		`SELECT id FROM source_import_items WHERE library_import_item_id=? ORDER BY id`, id)
	if err != nil {
		return nil, wrapErr(err)
	}
	links := make([]application.Scope, 0, len(ids))
	for _, id := range ids {
		links = append(links, application.Scope{Type: application.ScopeSourceImportItem, ID: id})
	}
	return links, nil
}
