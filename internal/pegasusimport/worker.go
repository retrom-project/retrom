package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	repository "retrom/internal/persistence/pegasusimport"
	libraryservice "retrom/internal/service/libraryimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/persistence/recordstore"
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
	if err := service.updateExecutionPhase(ctx, unit.ImportID, "COPYING_CONTENT"); err != nil {
		service.closeItemWithFailure(
			ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true,
			service.itemFailure("STORAGE", "UPDATE_IMPORT_PHASE", err, firstSourcePath(item)),
		)
		return
	}
	if !service.copyExecutionFiles(ctx, unit, root, &item) {
		return
	}
	service.copyExecutionAssets(ctx, unit, root, &item)
	if service.importCancelled(ctx, unit.ImportID) {
		service.closeItem(ctx, unit, item.ID, "CANCELLED", "CANCELLED", false)
		return
	}
	service.prepareReviewItem(ctx, unit, root, item)
}

func (service *Service) updateExecutionPhase(ctx context.Context, importID, phase string) error {
	now := service.now().UnixMilli()
	if _, err := recordstore.UpdatePegasusImports(ctx, service.database, recordstore.Update{
		Set: `phase=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='RUNNING' AND phase IS NOT ?`,
			Args:  []any{importID, phase},
		},
		Values: []any{phase, now},
	}); err != nil {
		return fmt.Errorf("pegasusimport/update execution phase: %w", err)
	}
	return nil
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
		blobID, err := service.recordCopiedFile(ctx, item.ID, item.Files[index].Ordinal, metadata)
		if err != nil {
			service.closeItem(ctx, unit, item.ID, "COMMIT_FAILED", "INTERNAL_ERROR", true)
			return false
		}
		item.Files[index].BlobID = blobID
	}
	return true
}

func (service *Service) copyExecutionAssets(
	ctx context.Context,
	unit work,
	root Root,
	item *executionItem,
) {
	for index := range item.Assets {
		metadata, valid, err := service.copyAsset(ctx, root, unit.RelativePath, item.Assets[index])
		if err != nil || !valid {
			service.closeAssetWarning(ctx, item.ID, item.Assets[index].Kind, mediaWarning(item.Assets[index].Kind, err))
			continue
		}
		blobID, err := service.recordCopiedAsset(
			ctx, item.ID, item.Assets[index].Kind, metadata, item.Assets[index].MediaType,
		)
		if err != nil {
			service.closeAssetWarning(ctx, item.ID, item.Assets[index].Kind, "PEGASUS_MEDIA_READ_FAILED")
			continue
		}
		item.Assets[index].BlobID = blobID
	}
}

func (service *Service) importCancelled(ctx context.Context, importID string) bool {
	var aggregateState string
	err := service.database.QueryRowContext(
		ctx, `SELECT state FROM pegasus_imports WHERE id=?`, importID,
	).Scan(&aggregateState)
	return err != nil || aggregateState == "CANCEL_REQUESTED"
}
