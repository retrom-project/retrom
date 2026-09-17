package maintenance

import (
	"context"
	"fmt"
	model "retrom/internal/model/maintenance"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
)

// ScheduleRestoredPayloads runs after source termination in the same restore transaction.
// The shared scheduler keeps materialized reviews and retryable sources retained.
func ScheduleRestoredPayloads(ctx context.Context, scope model.RestoredPayloadScope, now int64) error {
	scheduler := payloadreleaseservice.NewScheduler(nil)
	for _, kind := range []payloadreleasemodel.ScopeType{payloadreleasemodel.ScopePegasusImportItem, payloadreleasemodel.ScopeEmulationStationImportItem} {
		query := model.RestoredPayloadQuery{Kind: kind, Limit: 100}
		for {
			ids, err := scope.Records.RetainedSources(ctx, query)
			if err != nil {
				return fmt.Errorf("read restored payload owners: %w", err)
			}
			if len(ids) == 0 {
				break
			}
			for _, id := range ids {
				if id <= query.AfterID {
					return model.ErrInvalidBundle
				}
				if _, err := scheduler.TerminalSource(ctx, scope.Scheduling, payloadreleasemodel.Scope{Type: kind, ID: id}, now); err != nil {
					return fmt.Errorf("schedule restored source payload: %w", err)
				}
				query.AfterID = id
			}
		}
	}
	return nil
}
