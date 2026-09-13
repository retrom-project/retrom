package emulationstationimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	library "retrom/internal/service/libraryimport"
)

func (service *ReviewPreparer) accept(ctx context.Context, unit Execution, item ExecutionItem,
	result library.ServerImportResult,
) error {
	if result.Created.ImportJobID == "" || len(result.Items) != 1 || result.Items[0].ItemID == "" {
		return ErrInvalid
	}
	imported := result.Items[0]
	if err := service.dependencies.Items.Resume(
		ctx,
		unit,
		item.ID,
		result.Created.ImportJobID,
		imported.ItemID,
	); err != nil {
		return fmt.Errorf("resume EmulationStation owned source identity: %w", err)
	}
	if imported.ExistingGameID != "" {
		return service.duplicate(ctx, unit, item, imported)
	}
	if imported.State != "REVIEW_PENDING" {
		return service.block(ctx, unit, item, "EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED", nil)
	}
	var metadata library.ServerMetadata
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		details := service.failure("METADATA", "DECODE_FROZEN_METADATA", err, reviewSourcePath(item))
		details.LibraryImportJobID = &result.Created.ImportJobID
		details.LibraryImportItemID = &imported.ItemID
		return service.finish(ctx, unit, item, ItemOutcome{
			State: "BLOCKED_CONTENT", Code: "EMULATIONSTATION_METADATA_SYNTAX_INVALID", Failure: details,
		}, err)
	}
	if err := service.dependencies.Phases.SetPhase(ctx, unit, "PREPARING_REVIEWS"); err != nil {
		return fmt.Errorf("set EmulationStation review phase: %w", err)
	}
	err := service.dependencies.Handoff.Complete(ctx, ReviewHandoffRequest{
		Execution: unit, ItemID: item.ID, LibraryJobID: result.Created.ImportJobID, LibraryItemID: imported.ItemID,
	})
	if err == nil {
		return nil
	}
	details := service.failure("METADATA", "COMPLETE_REVIEW_HANDOFF", err, reviewSourcePath(item))
	details.LibraryImportJobID = &result.Created.ImportJobID
	details.LibraryImportItemID = &imported.ItemID
	return service.recordFailure(ctx, unit, item, "INTERNAL_ERROR", details, err)
}

func (service *ReviewPreparer) duplicate(ctx context.Context, unit Execution, item ExecutionItem,
	imported library.ServerImportItem,
) error {
	if imported.State != "DISCARDED" || len(imported.ExistingMatches) == 0 {
		return ErrInvalid
	}
	matches := make([]ExistingMatch, 0, len(imported.ExistingMatches))
	for _, match := range imported.ExistingMatches {
		matches = append(matches, ExistingMatch{GameID: match.GameID})
	}
	if err := service.dependencies.Items.Finish(ctx, unit, item.ID, ItemOutcome{
		State: "SKIPPED_EXISTING", ExistingGameID: imported.ExistingGameID, ExistingMatches: matches,
	}); err != nil {
		return fmt.Errorf("finish EmulationStation duplicate: %w", err)
	}
	return nil
}

func (service *ReviewPreparer) recordFailure(ctx context.Context, unit Execution, item ExecutionItem,
	code string, details *FailureDetails, cause error,
) error {
	if stop := reviewStopCause(ctx, cause); stop != nil {
		return stop
	}
	return service.finish(ctx, unit, item, ItemOutcome{
		State: "COMMIT_FAILED", Code: code, Retryable: true, Failure: details,
	}, cause)
}

func (service *ReviewPreparer) block(
	ctx context.Context,
	unit Execution,
	item ExecutionItem,
	code string,
	cause error,
) error {
	return service.finish(ctx, unit, item, ItemOutcome{State: "BLOCKED_CONTENT", Code: code}, cause)
}

func (service *ReviewPreparer) finish(ctx context.Context, unit Execution, item ExecutionItem,
	outcome ItemOutcome, cause error,
) error {
	if err := service.dependencies.Items.Finish(ctx, unit, item.ID, outcome); err != nil {
		return fmt.Errorf("persist EmulationStation review result: %w", errors.Join(cause, err))
	}
	return nil
}

func reviewStopCause(ctx context.Context, cause error) error {
	if stop := importStopCause(ctx, cause); stop != nil {
		return stop
	}
	if errors.Is(cause, library.ErrVersionConflict) {
		return cause
	}
	return nil
}
