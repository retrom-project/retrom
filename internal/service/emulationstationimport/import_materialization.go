package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
)

func (executor *ImportExecutor) CopyFiles(ctx context.Context, unit Execution, item *ExecutionItem) (bool, error) {
	for index, file := range item.Files {
		if file.State == "COPIED" && file.BlobID != "" {
			continue
		}
		if file.State != "DISCOVERED" {
			return false, ErrVersionConflict
		}
		blob, err := executor.dependencies.Sources.CopyFile(ctx, unit, file)
		if err != nil {
			return false, executor.sourceFailure(ctx, unit, *item, file.Path, err)
		}
		id, err := executor.dependencies.Materials.Copy(ctx, unit, fileMaterialSource(item.ID, file), blob)
		if err != nil {
			return false, executor.storageFailure(ctx, unit, *item, file.Path, "RECORD_COPIED_FILE", err)
		}
		item.Files[index].BlobID, item.Files[index].State = id, "COPIED"
	}
	return true, nil
}

func (executor *ImportExecutor) copyAssets(ctx context.Context, unit Execution, item *ExecutionItem) error {
	for index, asset := range item.Assets {
		blob, valid, err := executor.dependencies.Sources.CopyAsset(ctx, unit, asset)
		if stop := importStopCause(ctx, err); stop != nil {
			return stop
		}
		source := assetMaterialSource(item.ID, asset)
		if err != nil || !valid {
			if failure := executor.dependencies.Materials.Warning(
				ctx,
				unit,
				source,
				importMediaWarning(asset.Kind, err),
			); failure != nil {
				return fmt.Errorf("persist EmulationStation media warning: %w", errors.Join(err, failure))
			}
			continue
		}
		id, err := executor.dependencies.Materials.Copy(ctx, unit, source, blob)
		if stop := importStopCause(ctx, err); stop != nil {
			return stop
		}
		if err != nil {
			if failure := executor.dependencies.Materials.Warning(
				ctx,
				unit,
				source,
				"EMULATIONSTATION_MEDIA_READ_FAILED",
			); failure != nil {
				return fmt.Errorf("persist EmulationStation media storage warning: %w", errors.Join(err, failure))
			}
			continue
		}
		item.Assets[index].BlobID, item.Assets[index].State = id, "COPIED"
	}
	return nil
}

func fileMaterialSource(itemID string, file ExecutionFile) MaterialSource {
	return MaterialSource{
		Key:   MaterialKey{ItemID: itemID, Ordinal: file.Ordinal},
		Path:  file.Path,
		Facts: file.Facts,
		Size:  file.Size,
	}
}

func assetMaterialSource(itemID string, asset ExecutionAsset) MaterialSource {
	return MaterialSource{
		Key:       MaterialKey{ItemID: itemID, Kind: asset.Kind},
		Path:      asset.Path,
		Facts:     asset.Facts,
		Size:      asset.Size,
		MediaType: asset.MediaType,
		Width:     asset.Width,
		Height:    asset.Height,
	}
}

func importMediaWarning(kind string, err error) string {
	if errors.Is(err, ErrSourceChanged) {
		return "EMULATIONSTATION_SOURCE_CHANGED"
	}
	if kind == "COVER" {
		return "EMULATIONSTATION_IMAGE_INVALID"
	}
	return "EMULATIONSTATION_VIDEO_UNSUPPORTED"
}

func (executor *ImportExecutor) sourceFailure(
	ctx context.Context,
	unit Execution,
	item ExecutionItem,
	path string,
	cause error,
) error {
	if stop := importStopCause(ctx, cause); stop != nil {
		return stop
	}
	outcome := ItemOutcome{
		State:     "READ_FAILED",
		Code:      "READ_FAILED",
		Retryable: true,
		Failure:   executor.failure("SOURCE", "COPY_SOURCE", cause, path),
	}
	if errors.Is(cause, ErrSourceChanged) {
		outcome.State, outcome.Code, outcome.Retryable = "SOURCE_CHANGED", "EMULATIONSTATION_SOURCE_CHANGED", false
	}
	return executor.finishFailure(ctx, unit, item.ID, outcome, cause)
}

func (executor *ImportExecutor) storageFailure(
	ctx context.Context,
	unit Execution,
	item ExecutionItem,
	path, operation string,
	cause error,
) error {
	if stop := importStopCause(ctx, cause); stop != nil {
		return stop
	}
	outcome := ItemOutcome{
		State:     "COMMIT_FAILED",
		Code:      "INTERNAL_ERROR",
		Retryable: true,
		Failure:   executor.failure("STORAGE", operation, cause, path),
	}
	return executor.finishFailure(ctx, unit, item.ID, outcome, cause)
}

func (executor *ImportExecutor) finishFailure(
	ctx context.Context,
	unit Execution,
	id string,
	outcome ItemOutcome,
	cause error,
) error {
	if err := executor.dependencies.Items.Finish(ctx, unit, id, outcome); err != nil {
		return fmt.Errorf("persist EmulationStation item failure: %w", errors.Join(cause, err))
	}
	return nil
}

func (executor *ImportExecutor) failure(stage, operation string, cause error, path string) *FailureDetails {
	details := &FailureDetails{
		SchemaVersion:   1,
		Stage:           stage,
		Operation:       operation,
		CauseCode:       "INTERNAL_OPERATION_FAILED",
		TechnicalDetail: executor.dependencies.Diagnostics.Sanitize(cause),
	}
	if path != "" {
		details.RelativePath = &path
	}
	if code := executor.dependencies.Diagnostics.DatabaseCause(cause); code != "" {
		details.CauseCode = code
	}
	return details
}
