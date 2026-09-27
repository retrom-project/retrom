package sourcerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

func Relations(ctx context.Context, executor dbapi.Executor, facts *application.EffectOwner) error {
	err := dbapi.QueryRowContext(ctx, executor, `SELECT import_id,COALESCE(existing_game_id,'')
 FROM source_import_items WHERE id=?`, facts.Owner.Scope.ID).Scan(&facts.ParentID, &facts.ExistingGameID)
	if err != nil {
		return fmt.Errorf("read source release relations: %w", err)
	}
	return nil
}
