package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/libraryimport"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) attachLibraryResult(
	ctx context.Context,
	itemID, importJobID string,
	imported libraryimport.ServerImportItem,
) error {
	now := service.now().UnixMilli()
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, service.database, recordstore.Update{
		Set: `
execution_state='VALIDATING',library_import_job_id=?,library_import_item_id=?,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND execution_state='COPYING'`,
			Args:  []any{itemID},
		},
		Values: []any{importJobID, imported.ItemID, now},
	})
	if err != nil {
		return fmt.Errorf("emulationstationimport/attach library result: %w", err)
	}
	if rowsAffected(result) != 1 {
		return fmt.Errorf("emulationstationimport/attach library result: %w", errItemStateChanged)
	}
	return nil
}

func (service *Service) prepareReviewItem(ctx context.Context, unit work, _ Root, item executionItem) error {
	return service.reviewPreparer().Create(ctx, unit, item)
}

func (service *Service) prepareLibraryReview(ctx context.Context, unit work, item executionItem,
	jobID string, imported libraryimport.ServerImportItem,
) error {
	return service.reviewHandoff().Complete(ctx, application.ReviewHandoffRequest{
		Execution: unit, ItemID: item.ID, LibraryJobID: jobID, LibraryItemID: imported.ItemID,
	})
}
