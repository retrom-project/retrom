package payloadpurge

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/cleanupjobs"
)

func Consume(ctx context.Context, executor dbapi.Executor, change application.EffectConsumptionChange) error {
	before := change.Before
	result, err := executor.ExecContext(ctx,
		`UPDATE upload_consumptions SET released_at_ms=?,release_reason=?,version=version+1
 WHERE id=? AND version=? AND released_at_ms IS NULL AND upload_session_id=? AND
 COALESCE(upload_file_id,'')=? AND consumer_type=? AND consumer_id=?`, change.NowMS, change.Reason,
		before.ID, before.Version, before.SessionID, before.FileID, before.ConsumerType, before.ConsumerID)
	if err := releaseops.Count(result, err, 1); err != nil {
		return fmt.Errorf("write released consumption: %w", err)
	}
	return nil
}
