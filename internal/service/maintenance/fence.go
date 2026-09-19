package maintenance

import (
	"context"
	"fmt"

	model "retrom/internal/model/maintenance"

	"github.com/google/uuid"
)

func (service *Service) fenceRestore(ctx context.Context, path string) error {
	instant := service.now()
	now := instant.UnixMilli()
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create restore audit identity: %w", err)
	}
	err = service.repository.WithRestore(ctx, path, func(records model.RestoreRecords) error {
		access, err := records.RevokeAccess(ctx, now)
		if err != nil {
			return fmt.Errorf("revoke restored access: %w", err)
		}
		resources := records.Imports()
		if err := CompleteRestoredReviews(ctx, resources.Reviews, instant); err != nil {
			return fmt.Errorf("preserve restored reviews: %w", err)
		}
		imports, err := records.StopExternalImports(ctx, now)
		if err != nil {
			return fmt.Errorf("stop restored external imports: %w", err)
		}
		if err := ScheduleRestoredPayloads(ctx, resources.Payloads, now); err != nil {
			return fmt.Errorf("schedule restored terminal payloads: %w", err)
		}
		if err := records.StopBulkApprovals(ctx, now); err != nil {
			return fmt.Errorf("stop restored bulk approvals: %w", err)
		}
		if err := records.Audit(ctx, model.FenceAudit{ID: id.String(), Now: now, Counts: model.FenceCounts{
			Sessions: access.Sessions, Links: access.Links, Launches: access.Launches,
			BIOS: imports.BIOS, Pegasus: imports.Pegasus, EmulationStation: imports.EmulationStation,
		}}); err != nil {
			return fmt.Errorf("record restore security audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply restore security boundary: %w", err)
	}
	return nil
}
