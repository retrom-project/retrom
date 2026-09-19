package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"
	libraryimportmodel "retrom/internal/model/libraryimport"
	library "retrom/internal/service/libraryimport"
)

func completeExecutionReviews(
	ctx context.Context, scope model.ExecutionReviewScope, before model.LeaseSnapshot, clock func() time.Time,
) (model.LeaseSnapshot, bool, error) {
	if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" {
		return before, false, nil
	}
	reviews, err := scope.Read.Reviews(ctx, before.ImportID, 101)
	if err != nil {
		return model.LeaseSnapshot{}, false, fmt.Errorf("read interrupted EmulationStation reviews: %w", err)
	}
	for _, review := range reviews[:min(len(reviews), 100)] {
		preparation, err := model.ReviewPreparation(review)
		if err != nil {
			return model.LeaseSnapshot{}, false, fmt.Errorf("%w", err)
		}
		now := clock().UnixMilli()
		if err := scope.Write.Fence(ctx, before, now); err != nil {
			return model.LeaseSnapshot{}, false, fmt.Errorf("fence interrupted EmulationStation review: %w", err)
		}
		var metadata libraryimportmodel.ServerMetadata
		if err := json.Unmarshal([]byte(review.MetadataJSON), &metadata); err != nil {
			return model.LeaseSnapshot{}, false, fmt.Errorf("decode interrupted EmulationStation review metadata: %w", err)
		}
		_, additions, err := library.NewMetadataSeeder(nil, clock).SeedInScope(
			ctx, scope.Metadata, review.ReservedItemID, metadata, before.ReleaseYearMax)
		if err != nil {
			return model.LeaseSnapshot{}, false, fmt.Errorf("seed interrupted EmulationStation review: %w", err)
		}
		warnings, err := AppendReviewMetadataWarnings(review.WarningsJSON, additions)
		if err != nil {
			return model.LeaseSnapshot{}, false, err
		}
		err = scope.Write.CompleteReview(ctx, model.ExecutionReviewCompletion{
			Before: before, Review: review, WarningsJSON: warnings, Preparation: preparation, NowMS: now,
		})
		if err != nil {
			return model.LeaseSnapshot{}, false, fmt.Errorf("complete interrupted EmulationStation review: %w", err)
		}
		before, err = currentExecution(ctx, scope.Read, before.Execution)
		if err != nil {
			return model.LeaseSnapshot{}, false, err
		}
	}
	return before, len(reviews) > 100, nil
}

func AppendReviewMetadataWarnings(
	encoded string, additions []libraryimportmodel.ServerMetadataWarning,
) (string, error) {
	var warnings []map[string]any
	if err := json.Unmarshal([]byte(encoded), &warnings); err != nil {
		return "", fmt.Errorf("decode EmulationStation warnings: %w", err)
	}
	if warnings == nil {
		return "", model.ErrInvalid
	}
	for _, addition := range additions {
		if !hasExecutionWarning(warnings, addition) {
			warnings = append(warnings, map[string]any{"code": addition.Code, "field": addition.Field})
		}
	}
	warnings = BoundedWarnings(warnings)
	value, err := json.Marshal(warnings)
	if err != nil {
		return "", fmt.Errorf("encode EmulationStation warnings: %w", err)
	}
	return string(value), nil
}

func hasExecutionWarning(warnings []map[string]any, addition libraryimportmodel.ServerMetadataWarning) bool {
	for _, existing := range warnings {
		if existing["code"] == addition.Code && existing["field"] == addition.Field {
			return true
		}
	}
	return false
}
