package maintenance

import (
	"context"
	"fmt"

	release "retrom/internal/service/payloadrelease"
)

type RestoredImportScope struct {
	Reviews  RestoredReviewScope
	Payloads RestoredPayloadScope
}

type RestoredPayloadQuery struct {
	Kind    release.ScopeType
	AfterID string
	Limit   int
}

type RestoredPayloadRecords interface {
	RetainedSources(context.Context, RestoredPayloadQuery) ([]string, error)
}

type RestoredPayloadScope struct {
	Records    RestoredPayloadRecords
	Scheduling release.SchedulingScope
}

// ScheduleRestoredPayloads runs after source termination in the same restore transaction.
// The shared scheduler keeps materialized reviews and retryable sources retained.
func ScheduleRestoredPayloads(ctx context.Context, scope RestoredPayloadScope, now int64) error {
	scheduler := release.NewScheduler(nil)
	for _, kind := range []release.ScopeType{release.ScopeSourceImportItem} {
		query := RestoredPayloadQuery{Kind: kind, Limit: 100}
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
					return ErrInvalidBundle
				}
				if _, err := scheduler.TerminalSource(ctx, scope.Scheduling, release.Scope{Type: kind, ID: id}, now); err != nil {
					return fmt.Errorf("schedule restored source payload: %w", err)
				}
				query.AfterID = id
			}
		}
	}
	return nil
}
