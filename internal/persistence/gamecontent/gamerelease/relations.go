package gamerelease

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/cleanupjobs"
)

func Relations(ctx context.Context, executor dbapi.Executor, facts *application.EffectOwner) error {
	err := dbapi.QueryRowContext(ctx, executor, `
SELECT metadata_source_kind,content_source_kind,source_manifest_digest FROM games WHERE id=?`,
		facts.Owner.Scope.ID).Scan(
		&facts.MetadataSource.Kind, &facts.ContentSource.Kind, &facts.GameManifestDigest)
	if err != nil {
		return fmt.Errorf("read game release relations: %w", err)
	}
	return nil
}
