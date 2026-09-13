package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"

	"retrom/internal/libraryimport"
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

func (service *Service) closeItem(ctx context.Context, unit work, itemID, state, code string,
	retryable bool,
) error {
	return service.closeItemWithFailure(ctx, unit, itemID, state, code, retryable, nil)
}

func (service *Service) closeItemWithFailure(ctx context.Context, unit work, itemID, state, code string,
	retryable bool, failure *FailureDetails,
) error {
	return service.finishItemOutcome(ctx, unit, itemID, application.ItemOutcome{
		State: state, Code: code, Retryable: retryable, Failure: failure,
	})
}

func (service *Service) closeCancelled(ctx context.Context, unit work) (bool, error) {
	closed, err := service.executionControl().CloseCancelled(ctx, unit)
	if err != nil {
		return false, fmt.Errorf("close cancelled EmulationStation execution: %w", err)
	}
	return closed, nil
}
