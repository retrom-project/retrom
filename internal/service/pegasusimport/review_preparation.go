package pegasusimport

import (
	"context"
	"errors"
	"fmt"

	model "retrom/internal/model/pegasusimport"

	"retrom/internal/capability/content/contentcapability"
	library "retrom/internal/model/libraryimport"
)

type (
	ReviewSourceCreator interface {
		LookupOwnedServerSource(context.Context, library.SourceCreationIntent) (library.ServerImportResult, bool, error)
		CreateOwnedServerSource(context.Context, library.OwnedServerSourceRequest) (library.ServerImportResult, error)
	}
	ReviewItemTransitions interface {
		Resume(context.Context, model.ExecutionIdentity, string, string, string) error
		Finish(context.Context, model.ExecutionIdentity, string, model.ItemOutcome) error
	}
	ReviewCompleter interface {
		Complete(context.Context, model.ReviewHandoffRequest) error
	}
	ReviewPreparation struct {
		sources ReviewSourceCreator
		items   ReviewItemTransitions
		handoff ReviewCompleter
	}
)

func NewReviewPreparation(
	sources ReviewSourceCreator,
	items ReviewItemTransitions,
	handoff ReviewCompleter,
) *ReviewPreparation {
	return &ReviewPreparation{sources: sources, items: items, handoff: handoff}
}

func sourceIntent(unit model.Work, item model.ExecutionItem) library.SourceCreationIntent {
	paths := make([]string, 0, len(item.Files))
	for _, file := range item.Files {
		paths = append(paths, file.Path)
	}
	return library.SourceCreationIntent{
		Kind:     library.SourceOwnerPegasus,
		ImportID: unit.ImportID, ItemID: item.ID, JobID: unit.JobID,
		WorkerID: unit.WorkerID, ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt, PrimaryPaths: paths,
	}
}

// Resume precedes host/CAS reads, so retained identities can be replayed after payload cleanup.
func (service *ReviewPreparation) Resume(ctx context.Context, unit model.Work, item model.ExecutionItem) (bool, error) {
	result, found, err := service.sources.LookupOwnedServerSource(ctx, sourceIntent(unit, item))
	if err != nil {
		return false, fmt.Errorf("lookup Pegasus bound review: %w", err)
	}
	if !found {
		return false, nil
	}
	if err := service.accept(ctx, unit, item, result); err != nil {
		return true, err
	}
	return true, nil
}

func (service *ReviewPreparation) Create(
	ctx context.Context,
	unit model.Work,
	item model.ExecutionItem,
	files []library.ServerSourceFile,
) error {
	mode := contentcapability.ModeStandard
	if len(item.Files) > 1 {
		mode = contentcapability.ModeMultiDisc
	}
	result, err := service.sources.CreateOwnedServerSource(ctx, library.OwnedServerSourceRequest{
		Intent: sourceIntent(unit, item), TargetPlatformInstanceID: item.TargetPlatformID, ContentMode: mode,
		Files: files, TagIDs: item.TagIDs, AssignedByUserID: unit.CreatedByUserID,
	})
	if errors.Is(err, library.ErrSourceGrouping) {
		return service.blockContent(ctx, unit, item)
	}
	if err != nil {
		return fmt.Errorf("create Pegasus owned review: %w", err)
	}
	return service.accept(ctx, unit, item, result)
}

func (service *ReviewPreparation) accept(
	ctx context.Context,
	unit model.Work,
	item model.ExecutionItem,
	result library.ServerImportResult,
) error {
	if result.Created.ImportJobID == "" || len(result.Items) != 1 || result.Items[0].ItemID == "" {
		return model.ErrInvalid
	}
	if err := service.acceptResult(ctx, unit, item, result); err != nil {
		return &ReviewPreparationError{
			LibraryJobID: result.Created.ImportJobID, LibraryItemID: result.Items[0].ItemID, Cause: err,
		}
	}
	return nil
}

func (service *ReviewPreparation) acceptResult(
	ctx context.Context,
	unit model.Work,
	item model.ExecutionItem,
	result library.ServerImportResult,
) error {
	imported := result.Items[0]
	err := service.items.Resume(ctx, unit.Identity(), item.ID, result.Created.ImportJobID, imported.ItemID)
	if err != nil {
		return fmt.Errorf("resume Pegasus review identity: %w", err)
	}
	if imported.ExistingGameID != "" {
		if imported.State != "DISCARDED" || len(imported.ExistingMatches) == 0 {
			return model.ErrInvalid
		}
		matches := make([]model.ExistingMatch, 0, len(imported.ExistingMatches))
		for _, match := range imported.ExistingMatches {
			matches = append(matches, model.ExistingMatch{GameID: match.GameID})
		}
		if err := service.items.Finish(ctx, unit.Identity(), item.ID, model.ItemOutcome{
			State: "SKIPPED_EXISTING", ExistingGameID: imported.ExistingGameID, ExistingMatches: matches,
		}); err != nil {
			return fmt.Errorf("complete Pegasus duplicate: %w", err)
		}
		return nil
	}
	if imported.State != "REVIEW_PENDING" {
		return service.blockContent(ctx, unit, item)
	}
	if err := service.handoff.Complete(ctx, model.ReviewHandoffRequest{
		ItemID: item.ID, ImportID: unit.ImportID, JobID: unit.JobID, WorkerID: unit.WorkerID,
		LibraryJobID: result.Created.ImportJobID, LibraryItemID: imported.ItemID,
		ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt,
	}); err != nil {
		return fmt.Errorf("complete Pegasus review preparation: %w", err)
	}
	return nil
}

func (service *ReviewPreparation) blockContent(ctx context.Context, unit model.Work, item model.ExecutionItem) error {
	if err := service.items.Finish(
		ctx,
		unit.Identity(),
		item.ID,
		model.ItemOutcome{State: "BLOCKED_CONTENT", Code: "PEGASUS_CONTENT_FORMAT_UNSUPPORTED"},
	); err != nil {
		return fmt.Errorf("complete unsupported Pegasus content: %w", err)
	}
	return nil
}
