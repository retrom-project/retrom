package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	repository "retrom/internal/persistence/pegasusimport"
	libraryservice "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"
)

type (
	executionItem  = application.ExecutionItem
	executionFile  = application.ExecutionFile
	executionAsset = application.ExecutionAsset
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, fmt.Errorf("pegasusimport/read cancelled: %w", reader.ctx.Err())
	default:
	}
	if len(buffer) > 8<<20 {
		buffer = buffer[:8<<20]
	}
	count, err := reader.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("pegasusimport/read source: %w", err)
	}
	return count, nil
}

func (service *Service) executeImport(ctx context.Context, unit work, root Root) {
	for {
		cancelled, err := service.closeCancelled(ctx, unit)
		if err != nil {
			service.fail(ctx, unit, "INTERNAL_ERROR", true)
			return
		}
		if cancelled {
			return
		}
		item, found, err := service.nextItem(ctx, unit)
		if errors.Is(err, ErrVersionConflict) {
			return
		}
		if err != nil {
			service.fail(ctx, unit, "INTERNAL_ERROR", true)
			return
		}
		if !found {
			if err := service.finishImport(ctx, unit); err != nil && !errors.Is(err, ErrVersionConflict) {
				service.fail(ctx, unit, "INTERNAL_ERROR", true)
			}
			return
		}
		service.processItem(ctx, unit, root, item)
	}
}

func (service *Service) nextItem(ctx context.Context, unit work) (executionItem, bool, error) {
	item, found, err := application.NewItemWork(repository.NewItemWork(service.database), service.now).Next(
		ctx,
		unit.Identity(),
	)
	if err != nil {
		return executionItem{}, false, fmt.Errorf("pegasusimport/next item: %w", err)
	}
	return item, found, nil
}

func (service *Service) processItem(ctx context.Context, unit work, root Root, item executionItem) {
	resumed, err := service.resumeLibraryReview(ctx, unit, item)
	if err != nil {
		if errors.Is(err, application.ErrVersionConflict) || errors.Is(err, libraryservice.ErrVersionConflict) {
			return
		}
		service.closeItemWithFailure(ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
			service.itemFailure("REVIEW_HANDOFF", "RESUME_REVIEW_HANDOFF", err, firstSourcePath(item)))
		return
	}
	if resumed {
		return
	}
	if err := service.updateExecutionPhase(ctx, unit, "COPYING_CONTENT"); err != nil {
		service.closeItemWithFailure(
			ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
			service.itemFailure("STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item)),
		)
		return
	}
	if !service.copyExecutionFiles(ctx, unit, root, &item) {
		return
	}
	if !service.copyExecutionAssets(ctx, unit, root, &item) {
		return
	}
	cancelled, err := service.importCancelled(ctx, unit)
	if err != nil {
		service.closeItemWithFailure(
			ctx,
			unit,
			item.ID,
			"COMMIT_FAILED",
			"INTERNAL_ERROR",
			true,
			service.itemFailure("STORAGE", "READ_CANCELLATION", err, firstSourcePath(item)),
		)
		return
	}
	if cancelled {
		service.closeItem(ctx, unit, item.ID, "CANCELLED", "CANCELLED", false)
		return
	}
	service.prepareReviewItem(ctx, unit, root, item)
}

func (service *Service) copyExecutionFiles(
	ctx context.Context,
	unit work,
	root Root,
	item *executionItem,
) bool {
	for index := range item.Files {
		metadata, err := service.copySource(
			ctx,
			root,
			unit.RelativePath,
			item.Files[index].Path,
			item.Files[index].Size,
			item.Files[index].Facts,
		)
		if err != nil {
			code := "READ_FAILED"
			if errors.Is(err, ErrSourceChanged) {
				code = "PEGASUS_SOURCE_CHANGED"
			}
			service.closeItem(ctx, unit, item.ID, terminalForCode(code), code, !errors.Is(err, ErrSourceChanged))
			return false
		}
		blobID, err := service.recordCopiedFile(ctx, unit, item.ID, item.Files[index], metadata)
		if err != nil {
			service.closeItem(ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true)
			return false
		}
		item.Files[index].BlobID = blobID
	}
	return true
}

func (service *Service) copyExecutionAssets(ctx context.Context, unit work, root Root, item *executionItem) bool {
	for index := range item.Assets {
		asset := item.Assets[index]
		metadata, valid, err := service.copyAsset(ctx, root, unit.RelativePath, asset)
		if err != nil && !errors.Is(err, ErrSourceChanged) {
			service.closeItemWithFailure(ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
				service.itemFailure("STORAGE", "COPY_MEDIA", err, asset.Path))
			return false
		}
		if err != nil || !valid {
			if err := service.closeAssetWarning(ctx, unit, item.ID, asset, mediaWarning(asset.Kind, err)); err != nil {
				service.closeItemWithFailure(ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
					service.itemFailure("STORAGE", "WRITE_MEDIA_WARNING", err, asset.Path))
				return false
			}
			continue
		}
		blobID, err := service.recordCopiedAsset(ctx, unit, item.ID, asset, metadata)
		if err != nil {
			service.closeItemWithFailure(ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
				service.itemFailure("STORAGE", "BIND_MEDIA", err, asset.Path))
			return false
		}
		item.Assets[index].BlobID = blobID
	}
	return true
}
