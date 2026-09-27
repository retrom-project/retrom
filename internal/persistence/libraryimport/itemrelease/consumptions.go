package itemrelease

import (
	"context"

	dbapi "retrom/internal/database"
	uploads "retrom/internal/persistence/uploads/payloadpurge"
	application "retrom/internal/service/payloadrelease"
)

func Consumptions(ctx context.Context, executor dbapi.Executor, id string) ([]application.EffectConsumption, error) {
	return wrapPair(uploads.ReadConsumptions(ctx, executor, `
SELECT id,upload_session_id,COALESCE(upload_file_id,''),consumer_type,consumer_id,version,released_at_ms
FROM upload_consumptions WHERE
consumer_type='REVIEW_ASSET' AND consumer_id IN (SELECT id FROM review_uploaded_assets WHERE import_item_id=?) OR
consumer_type='REVIEW_ARCADE_PARENT' AND consumer_id IN (
 SELECT id FROM review_arcade_parent_attachments WHERE import_item_id=?
) OR
consumer_type='REVIEW_MULTI_DISC' AND consumer_id IN (
 SELECT id FROM review_multidisc_attachments WHERE import_item_id=?
)
`, id, id, id))
}
