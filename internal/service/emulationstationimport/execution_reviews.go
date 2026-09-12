package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	library "retrom/internal/service/libraryimport"
)

type ExecutionReview struct {
	ItemID, State, LibraryJobID, LibraryItemID, ReservedJobID, ReservedItemID string
	MetadataJSON, WarningsJSON                                                string
	Version                                                                   int64
}

type ExecutionReviewCompletion struct {
	Before       LeaseSnapshot
	Review       ExecutionReview
	WarningsJSON string
	NowMS        int64
}

func (service *ExecutionControl) completeReviews(
	ctx context.Context, scope ExecutionScope, before LeaseSnapshot,
) (LeaseSnapshot, bool, error) {
	if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" {
		return before, false, nil
	}
	reviews, err := scope.Read.Reviews(ctx, before.ImportID, 101)
	if err != nil {
		return LeaseSnapshot{}, false, fmt.Errorf("read interrupted EmulationStation reviews: %w", err)
	}
	for _, review := range reviews[:min(len(reviews), 100)] {
		now := service.now().UnixMilli()
		if err := scope.Write.Fence(ctx, before, now); err != nil {
			return LeaseSnapshot{}, false, fmt.Errorf("fence interrupted EmulationStation review: %w", err)
		}
		var metadata library.ServerMetadata
		if err := json.Unmarshal([]byte(review.MetadataJSON), &metadata); err != nil {
			return LeaseSnapshot{}, false, fmt.Errorf("decode interrupted EmulationStation review metadata: %w", err)
		}
		_, additions, err := library.NewMetadataSeeder(nil, service.now).SeedInScope(
			ctx, scope.Metadata, review.ReservedItemID, metadata, before.ReleaseYearMax)
		if err != nil {
			return LeaseSnapshot{}, false, fmt.Errorf("seed interrupted EmulationStation review: %w", err)
		}
		warnings, err := appendExecutionWarnings(review.WarningsJSON, additions)
		if err != nil {
			return LeaseSnapshot{}, false, err
		}
		err = scope.Write.CompleteReview(ctx, ExecutionReviewCompletion{
			Before: before, Review: review, WarningsJSON: warnings, NowMS: now,
		})
		if err != nil {
			return LeaseSnapshot{}, false, fmt.Errorf("complete interrupted EmulationStation review: %w", err)
		}
		before, err = currentExecution(ctx, scope.Read, before.Execution)
		if err != nil {
			return LeaseSnapshot{}, false, err
		}
	}
	return before, len(reviews) > 100, nil
}

func appendExecutionWarnings(encoded string, additions []library.ServerMetadataWarning) (string, error) {
	var warnings []map[string]any
	if err := json.Unmarshal([]byte(encoded), &warnings); err != nil {
		return "", fmt.Errorf("decode EmulationStation warnings: %w", err)
	}
	if warnings == nil {
		return "", ErrInvalid
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

func hasExecutionWarning(warnings []map[string]any, addition library.ServerMetadataWarning) bool {
	for _, existing := range warnings {
		if existing["code"] == addition.Code && existing["field"] == addition.Field {
			return true
		}
	}
	return false
}
