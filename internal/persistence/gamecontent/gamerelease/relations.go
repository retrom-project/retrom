package gamerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

func Relations(ctx context.Context, executor dbapi.Executor, facts *application.EffectOwner) error {
	err := dbapi.QueryRowContext(ctx, executor, `SELECT metadata_source_kind,COALESCE(metadata_source_ref_id,''),
 content_source_kind,COALESCE(content_source_ref_id,'') FROM games WHERE id=?`, facts.Owner.Scope.ID).Scan(
		&facts.MetadataSource.Kind, &facts.MetadataSource.ID, &facts.ContentSource.Kind, &facts.ContentSource.ID)
	if err != nil {
		return fmt.Errorf("read game release relations: %w", err)
	}
	return nil
}
