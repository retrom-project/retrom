package pegasusimport

import (
	"context"
	"fmt"
	"math"
	"time"

	library "retrom/internal/service/libraryimport"
)

type (
	ReviewHandoffRequest struct {
		ItemID, ImportID, JobID, LibraryJobID, LibraryItemID, WorkerID string
		ExecutionNo, Attempt                                           int64
	}
	ReviewHandoffSnapshot struct {
		Identity                                         ReviewHandoffRequest
		State, ImportState, JobState                     string
		Version, ImportVersion, LeaseUntilMS, DeadlineMS int64
		Metadata                                         library.ServerMetadata
		Warnings                                         []map[string]any
	}
	ReviewHandoffChange struct {
		Before   ReviewHandoffSnapshot
		Warnings []map[string]any
		NowMS    int64
	}
	ReviewHandoffRecords interface {
		CurrentReviewHandoff(context.Context, string) (ReviewHandoffSnapshot, error)
		FinishReviewHandoff(context.Context, ReviewHandoffChange) error
	}
	ReviewHandoffScope struct {
		Records  ReviewHandoffRecords
		Metadata library.MetadataScope
	}
	ReviewHandoffRepository interface {
		WithReviewHandoff(context.Context, func(ReviewHandoffScope) error) error
	}
	ReviewMetadataSeeder interface {
		SeedInScope(
			context.Context, library.MetadataScope, string, library.ServerMetadata, int,
		) (int64, []library.ServerMetadataWarning, error)
	}
	ReviewHandoff struct {
		repository ReviewHandoffRepository
		metadata   ReviewMetadataSeeder
		now        func() time.Time
	}
)

func NewReviewHandoff(
	repository ReviewHandoffRepository,
	metadata ReviewMetadataSeeder,
	now func() time.Time,
) *ReviewHandoff {
	return &ReviewHandoff{repository: repository, metadata: metadata, now: now}
}

func (service *ReviewHandoff) Complete(ctx context.Context, request ReviewHandoffRequest) error {
	err := service.repository.WithReviewHandoff(ctx, func(scope ReviewHandoffScope) error {
		before, err := scope.Records.CurrentReviewHandoff(ctx, request.ItemID)
		if err != nil {
			return fmt.Errorf("read Pegasus review handoff: %w", err)
		}
		if before.Identity != request || request.LibraryItemID == "" || request.LibraryJobID == "" {
			return ErrVersionConflict
		}
		if before.State == "REVIEW_PENDING" {
			return nil
		}
		now := service.now()
		if !canCompleteReviewHandoff(before, now.UnixMilli()) {
			return ErrVersionConflict
		}
		_, warnings, err := service.metadata.SeedInScope(
			ctx,
			scope.Metadata,
			request.LibraryItemID,
			before.Metadata,
			now.UTC().Year()+1,
		)
		if err != nil {
			return fmt.Errorf("seed Pegasus review metadata: %w", err)
		}
		change := ReviewHandoffChange{
			Before:   before,
			Warnings: mergeReviewMetadataWarnings(before.Warnings, warnings),
			NowMS:    now.UnixMilli(),
		}
		if err := scope.Records.FinishReviewHandoff(ctx, change); err != nil {
			return fmt.Errorf("save Pegasus review handoff: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete Pegasus review handoff: %w", err)
	}
	return nil
}

func canCompleteReviewHandoff(before ReviewHandoffSnapshot, now int64) bool {
	if before.State != "VALIDATING" || before.Version < 1 || before.Version == math.MaxInt64 ||
		before.ImportVersion < 1 || before.ImportVersion == math.MaxInt64 {
		return false
	}
	active := before.ImportState == "RUNNING" && before.JobState == "RUNNING"
	canceling := before.ImportState == "CANCEL_REQUESTED" && before.JobState == "CANCEL_REQUESTED"
	return (active || canceling) && before.Identity.ExecutionNo > 0 && before.Identity.Attempt > 0 &&
		before.Identity.WorkerID != "" &&
		before.LeaseUntilMS > now && before.DeadlineMS > now
}

func mergeReviewMetadataWarnings(
	existing []map[string]any,
	additions []library.ServerMetadataWarning,
) []map[string]any {
	result := make([]map[string]any, 0, len(existing)+len(additions))
	result = append(result, existing...)
	for _, addition := range additions {
		duplicate := false
		for _, warning := range result {
			if warning["code"] == addition.Code && warning["field"] == addition.Field {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, map[string]any{"code": addition.Code, "field": addition.Field})
		}
	}
	return result
}
