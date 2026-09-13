package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

type ImmediateGCResult = application.ImmediateGCResult

func (service *Service) ScheduleImmediateGC(ctx context.Context, actor string) (ImmediateGCResult, error) {
	result, err := service.gc.Immediate(ctx, actor)
	if err != nil {
		return ImmediateGCResult{}, fmt.Errorf("schedule immediate GC: %w", err)
	}
	return result, nil
}

func (service *Service) stageAllUnreferenced(ctx context.Context) error {
	if err := service.gc.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile GC candidates: %w", err)
	}
	return nil
}

func (service *Service) stageCandidates(ctx context.Context, tx *sql.Tx, ids []string) error {
	if err := service.gc.StageInScope(ctx, repository.BindGC(tx), ids); err != nil {
		return fmt.Errorf("stage GC candidates: %w", err)
	}
	return nil
}

// StageCandidates keeps reference removal and the GC handoff in the caller's transaction.
func (service *Service) StageCandidates(ctx context.Context, tx *sql.Tx, ids []string) error {
	return service.stageCandidates(ctx, tx, ids)
}

func (service *Service) executeBlobGC(ctx context.Context, job claimedJob) error {
	if err := service.garbage.Execute(ctx, application.Execution{Work: job.Work, Input: job.Input}); err != nil {
		return fmt.Errorf("execute garbage collection: %w", err)
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
