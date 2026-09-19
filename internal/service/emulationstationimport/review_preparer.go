package emulationstationimport

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/capability/content/contentcapability"
	model "retrom/internal/model/emulationstationimport"
	libraryimportmodel "retrom/internal/model/libraryimport"
)

type ReviewSourceCreator interface {
	LookupOwnedServerSource(context.Context, libraryimportmodel.SourceCreationIntent) (
		libraryimportmodel.ServerImportResult,
		bool,
		error,
	)
	CreateOwnedServerSource(context.Context, libraryimportmodel.OwnedServerSourceRequest) (
		libraryimportmodel.ServerImportResult,
		error,
	)
}
type ReviewCompanions interface {
	Files(context.Context, model.Execution, model.ExecutionItem) ([]libraryimportmodel.ServerSourceFile, error)
}
type ReviewItemTransitions interface {
	Resume(context.Context, model.Execution, string, string, string) error
	Finish(context.Context, model.Execution, string, model.ItemOutcome) error
}
type ReviewPhases interface {
	SetPhase(context.Context, model.Execution, string) error
}
type ReviewCompleter interface {
	Complete(context.Context, model.ReviewHandoffRequest) error
}
type ReviewPreparerDependencies struct {
	Sources     ReviewSourceCreator
	Companions  ReviewCompanions
	Items       ReviewItemTransitions
	Phases      ReviewPhases
	Handoff     ReviewCompleter
	Diagnostics FailureDiagnostics
}
type ReviewPreparer struct{ dependencies ReviewPreparerDependencies }

func NewReviewPreparer(dependencies ReviewPreparerDependencies) *ReviewPreparer {
	return &ReviewPreparer{dependencies: dependencies}
}

func reviewSourceIntent(unit model.Execution, item model.ExecutionItem) libraryimportmodel.SourceCreationIntent {
	paths := make([]string, 0, len(item.Files))
	for _, file := range item.Files {
		paths = append(paths, file.Path)
	}
	return libraryimportmodel.SourceCreationIntent{
		Kind:     libraryimportmodel.SourceOwnerEmulationStation,
		ImportID: unit.ImportID, ItemID: item.ID, JobID: unit.JobID, WorkerID: unit.WorkerID,
		ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt, PrimaryPaths: paths,
	}
}

// Resume checks permanent bindings before any source or CAS materialization.
func (service *ReviewPreparer) Resume(
	ctx context.Context,
	unit model.Execution,
	item model.ExecutionItem,
) (bool, error) {
	result, found, err := service.dependencies.Sources.LookupOwnedServerSource(ctx, reviewSourceIntent(unit, item))
	if err != nil {
		return false, fmt.Errorf("lookup EmulationStation owned review: %w", err)
	}
	if !found {
		return false, nil
	}
	if err := service.accept(ctx, unit, item, result); err != nil {
		return true, err
	}
	return true, nil
}

func (service *ReviewPreparer) Create(ctx context.Context, unit model.Execution, item model.ExecutionItem) error {
	files, err := service.files(ctx, unit, item)
	if err != nil {
		return service.recordFailure(ctx, unit, item, "INTERNAL_ERROR",
			service.failure("SOURCE_ASSEMBLY", "ASSEMBLE_SOURCE_FILES", err, reviewSourcePath(item)), err)
	}
	if err := service.dependencies.Phases.SetPhase(ctx, unit, "VALIDATING"); err != nil {
		return fmt.Errorf("set EmulationStation validation phase: %w", err)
	}
	mode := contentcapability.ModeStandard
	if item.ContentKind == contentcapability.ModeMultiDisc {
		mode = contentcapability.ModeMultiDisc
	}
	result, err := service.dependencies.Sources.CreateOwnedServerSource(ctx, libraryimportmodel.OwnedServerSourceRequest{
		Intent: reviewSourceIntent(unit, item), TargetPlatformInstanceID: item.TargetPlatformID, ContentMode: mode,
		Files: files, TagIDs: item.TagIDs, AssignedByUserID: unit.CreatedByUserID,
	})
	if err != nil {
		return service.creationFailure(ctx, unit, item, files, err)
	}
	return service.accept(ctx, unit, item, result)
}

func (service *ReviewPreparer) files(
	ctx context.Context,
	unit model.Execution,
	item model.ExecutionItem,
) ([]libraryimportmodel.ServerSourceFile, error) {
	result := make([]libraryimportmodel.ServerSourceFile, 0, len(item.Files))
	for _, file := range item.Files {
		result = append(result, libraryimportmodel.ServerSourceFile{
			RelativePath: file.Path,
			BlobID:       file.BlobID,
			SizeBytes:    file.Size,
		})
	}
	companions, err := service.dependencies.Companions.Files(ctx, unit, item)
	if err != nil {
		return nil, fmt.Errorf("assemble EmulationStation dependencies: %w", err)
	}
	return append(result, companions...), nil
}

func (service *ReviewPreparer) creationFailure(ctx context.Context, unit model.Execution, item model.ExecutionItem,
	files []libraryimportmodel.ServerSourceFile, cause error,
) error {
	if errors.Is(cause, libraryimportmodel.ErrMultiDiscModeUnavailable) {
		return service.block(ctx, unit, item, "MULTI_DISC_MODE_UNAVAILABLE", cause)
	}
	if errors.Is(cause, libraryimportmodel.ErrSourceGrouping) {
		return service.block(ctx, unit, item, "EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED", cause)
	}
	return service.recordFailure(ctx, unit, item, "EMULATIONSTATION_LIBRARY_IMPORT_FAILED",
		service.libraryFailure(cause, files), cause)
}
