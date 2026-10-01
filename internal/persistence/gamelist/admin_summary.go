package gamelist

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/gamelist"
)

const defaultRuntime = `(SELECT variant.status FROM game_variants variant
WHERE variant.game_id=g.id AND variant.core_id=pi.default_core_id LIMIT 1)`

func (repository *Repository) adminSummary(ctx context.Context) (*application.AdminSummary, error) {
	var result application.AdminSummary
	err := dbapi.QueryRowContext(ctx, repository.database, `SELECT count(*),
COALESCE(sum(g.status<>'DELETED' AND COALESCE(`+defaultRuntime+`,'')<>'READY'),0),
COALESCE(sum(NOT EXISTS(SELECT 1 FROM game_assets a WHERE a.game_id=g.id AND a.kind='COVER')),0),
COALESCE(sum(NOT(trim(g.description)<>'' AND trim(g.developer)<>'' AND trim(g.publisher)<>''
AND trim(g.genre)<>'' AND g.players IS NOT NULL AND g.release_year IS NOT NULL)),0),
COALESCE(sum(g.status<>'PUBLISHED'),0)
FROM games g JOIN platform_instances pi ON pi.id=g.platform_instance_id`).Scan(
		&result.Total, &result.RuntimeAttention, &result.MissingCover, &result.IncompleteMetadata, &result.Hidden)
	if err != nil {
		return nil, fmt.Errorf("read admin game summary: %w", err)
	}
	return &result, nil
}
