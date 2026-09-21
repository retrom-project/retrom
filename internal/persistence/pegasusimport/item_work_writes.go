package pegasusimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

func (records itemWorkRecords) Claim(ctx context.Context, change application.ItemClaim) error {
	result, err := recordstore.UpdatePegasusImportItems(ctx, records.tx, recordstore.Update{
		Set: `execution_state='COPYING',version=version+1,updated_at_ms=?`, Values: []any{change.NowMS},
		Scope: recordstore.Scope{Where: `id=? AND import_id=? AND version=? AND execution_state=?
AND execution_state='PENDING'` + itemExecutionFence, Args: itemFenceArgs(change.Before, change.NowMS)},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records itemWorkRecords) Resume(ctx context.Context, change application.ItemResume) error {
	args := itemFenceArgs(change.Before, change.NowMS)
	args = append(args, change.Before.Item.LibraryImportJobID, change.Before.Item.LibraryImportItemID)
	result, err := recordstore.UpdatePegasusImportItems(ctx, records.tx, recordstore.Update{
		Set: `execution_state='VALIDATING',version=version+1,updated_at_ms=?`, Values: []any{change.NowMS},
		Scope: recordstore.Scope{Where: `id=? AND import_id=? AND version=? AND execution_state=?
AND execution_state='COPYING'` + itemExecutionFence + `
AND library_import_job_id=? AND library_import_item_id=?`, Args: args},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records itemWorkRecords) Finish(ctx context.Context, change application.ItemFinish) error {
	outcome := change.Outcome
	failure, matches, err := encodeItemOutcome(outcome)
	if err != nil {
		return err
	}
	result, err := recordstore.UpdatePegasusImportItems(ctx, records.tx, recordstore.Update{
		Set: `execution_state=?,error_code=?,retryable=?,error_details_json=?,existing_game_id=COALESCE(?,existing_game_id),
existing_matches_json=COALESCE(?,existing_matches_json),completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{
			outcome.State, optionalText(outcome.Code), outcome.Retryable, failure,
			optionalText(outcome.ExistingGameID), matches, change.NowMS, change.NowMS,
		},
		Scope: recordstore.Scope{
			Where: `id=? AND import_id=? AND version=? AND execution_state=?
AND execution_state IN ('COPYING','VALIDATING')` + itemExecutionFence,
			Args: itemFenceArgs(change.Before, change.NowMS),
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	return RefreshCountsAndEvent(ctx, records.tx, change.Before.Execution.JobID,
		change.Before.Item.ImportID, change.Before.Item.ID, outcome.State, change.NowMS)
}

func encodeItemOutcome(outcome application.ItemOutcome) (*string, *string, error) {
	var failure, matches *string
	if outcome.Failure != nil {
		encoded, err := json.Marshal(outcome.Failure)
		if err != nil {
			return nil, nil, fmt.Errorf("encode Pegasus item failure: %w", err)
		}
		value := string(encoded)
		failure = &value
	}
	if outcome.ExistingMatches != nil {
		encoded, err := json.Marshal(outcome.ExistingMatches)
		if err != nil {
			return nil, nil, fmt.Errorf("encode Pegasus duplicate matches: %w", err)
		}
		value := string(encoded)
		matches = &value
	}
	return failure, matches, nil
}
