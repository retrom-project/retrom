package gamerelease

import (
	"context"

	dbapi "retrom/internal/database"
	uploads "retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/cleanupjobs"
)

func Consumptions(ctx context.Context, executor dbapi.Executor, id string) ([]application.EffectConsumption, error) {
	return wrapPair(uploads.ReadConsumptions(ctx, executor, `
SELECT id,upload_session_id,COALESCE(upload_file_id,''),consumer_type,consumer_id,version,released_at_ms
FROM upload_consumptions WHERE
consumer_type='GAME_ASSET' AND consumer_id IN (SELECT id FROM game_assets WHERE game_id=?)
`, id))
}
