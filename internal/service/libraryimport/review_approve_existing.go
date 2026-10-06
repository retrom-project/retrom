package libraryimport

import (
	"fmt"

	"retrom/internal/service/idempotency"
)

func (run *reviewApprovalRun) skipExisting(state *PublicationState) error {
	existing := &ReviewExistingContent{Identity: run.identityDigest, Games: run.duplicateGames}
	if run.request.Bulk != nil {
		existing.BulkID = &run.request.Bulk.BulkID
	}
	_, err := NewReviewDiscards(nil, run.service.now).DiscardInScope(run.ctx, run.scope.Discard, ReviewDiscardRequest{
		ItemID: run.request.ItemID, ExpectedVersion: run.request.ExpectedVersion,
		Existing: existing,
	})
	if err != nil {
		return err
	}
	state.State, state.GameID = "SKIPPED_EXISTING", run.duplicateGames[0].GameID
	if err := idempotency.Complete(run.ctx, idempotency.Result{
		Value: ReviewApproved{GameID: state.GameID, Status: state.State},
	}); err != nil {
		return fmt.Errorf("complete existing-content decision: %w", err)
	}
	return nil
}

func (run *reviewApprovalRun) prepareDecision() error {
	for _, step := range []func() error{run.load, run.claimDuplicates} {
		if err := step(); err != nil {
			return err
		}
	}
	run.now = run.service.now().UnixMilli()
	if run.request.Bulk != nil {
		if err := run.scope.Bulk.CheckRequest(run.ctx, run.request, run.now); err != nil {
			return fmt.Errorf("prepare bulk publication: %w", err)
		}
	}
	return nil
}
