package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/emulationstationimport"
	"time"

	library "retrom/internal/model/libraryimport"
)

type ReviewHandoff struct {
	repository model.ReviewHandoffRepository
	metadata   model.ReviewMetadataSeeder
	now        func() time.Time
}

func NewReviewHandoff(
	repository model.ReviewHandoffRepository,
	metadata model.ReviewMetadataSeeder,
	now func() time.Time,
) *ReviewHandoff {
	return &ReviewHandoff{repository: repository, metadata: metadata, now: now}
}

func (service *ReviewHandoff) Complete(ctx context.Context, request model.ReviewHandoffRequest) error {
	err := service.repository.WithReviewHandoff(ctx, func(scope model.ReviewHandoffScope) error {
		before, err := currentExecution(ctx, scope.Read, request.Execution)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		if err := validateImportExecution(before, request.Execution, now); err != nil {
			return err
		}
		review, found, err := scope.Read.Review(ctx, request.Execution.ImportID, request.ItemID)
		if err != nil {
			return fmt.Errorf("read EmulationStation review reservation: %w", err)
		}
		if !found || !matchesReviewHandoff(review, request) {
			return model.ErrVersionConflict
		}
		if review.State == "REVIEW_PENDING" {
			return nil
		}
		if review.State != "COPYING" && review.State != "VALIDATING" {
			return model.ErrVersionConflict
		}
		preparation, err := reviewPreparation(review)
		if err != nil {
			return err
		}
		return service.complete(
			ctx,
			scope,
			model.ExecutionReviewCompletion{Before: before, Review: review, Preparation: preparation, NowMS: now},
		)
	})
	if err != nil {
		return fmt.Errorf("complete EmulationStation review handoff: %w", err)
	}
	return nil
}

func matchesReviewHandoff(review model.ExecutionReview, request model.ReviewHandoffRequest) bool {
	if !validItemVersion(review.Version) || review.ItemID != request.ItemID ||
		request.LibraryJobID == "" || request.LibraryItemID == "" ||
		review.ReservedJobID != request.LibraryJobID || review.ReservedItemID != request.LibraryItemID {
		return false
	}
	if review.LibraryJobID == "" && review.LibraryItemID == "" {
		return review.State == "COPYING"
	}
	return review.LibraryJobID == request.LibraryJobID && review.LibraryItemID == request.LibraryItemID
}

func (service *ReviewHandoff) complete(
	ctx context.Context,
	scope model.ReviewHandoffScope,
	change model.ExecutionReviewCompletion,
) error {
	if err := scope.Write.Fence(ctx, change.Before, change.NowMS); err != nil {
		return fmt.Errorf("fence EmulationStation metadata handoff: %w", err)
	}
	var metadata library.ServerMetadata
	if err := json.Unmarshal([]byte(change.Review.MetadataJSON), &metadata); err != nil {
		return fmt.Errorf("decode frozen EmulationStation review metadata: %w", err)
	}
	_, additions, err := service.metadata.SeedInScope(
		ctx, scope.Metadata, change.Review.ReservedItemID, metadata, change.Before.ReleaseYearMax,
	)
	if err != nil {
		return fmt.Errorf("seed EmulationStation review metadata: %w", err)
	}
	change.WarningsJSON, err = appendExecutionWarnings(change.Review.WarningsJSON, additions)
	if err != nil {
		return err
	}
	if err := scope.Write.CompleteReview(ctx, change); err != nil {
		return fmt.Errorf("save EmulationStation review handoff: %w", err)
	}
	return nil
}
