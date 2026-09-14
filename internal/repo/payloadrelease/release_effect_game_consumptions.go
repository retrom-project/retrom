package payloadrelease

import (
	"context"

	application "retrom/internal/model/payloadrelease"
)

func (records effectRecords) gameConsumptions(ctx context.Context, id string) ([]application.EffectConsumption, error) {
	return records.readConsumptions(ctx, `
SELECT id,upload_session_id,COALESCE(upload_file_id,''),consumer_type,consumer_id,version,released_at_ms
FROM upload_consumptions WHERE
consumer_type='GAME_ASSET' AND consumer_id IN (SELECT id FROM game_assets WHERE game_id=?) OR
consumer_type='GAME_CONTENT_REPLACE_JOB' AND consumer_id=(
  SELECT content_source_ref_id FROM games WHERE id=? AND content_source_kind='ADMIN_REPLACE'
)
`, id, id)
}
