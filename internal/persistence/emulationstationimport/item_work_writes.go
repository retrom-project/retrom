package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/payloadrelease"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func (records itemWorkRecords) Claim(ctx context.Context, change application.ItemClaim) error {
	if err := records.fence(ctx, change.Before, change.NowMS); err != nil {
		return err
	}
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state='COPYING',version=version+1,updated_at_ms=?`, Values: []any{change.NowMS},
		Scope: recordstore.Scope{
			Where: ownedItemPredicate + ` AND execution_state='PENDING'`,
			Args:  ownedItemArguments(change.Before),
		},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records itemWorkRecords) Resume(ctx context.Context, change application.ItemResume) error {
	if err := records.fence(ctx, change.Before, change.NowMS); err != nil {
		return err
	}
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state='VALIDATING',version=version+1,updated_at_ms=?`, Values: []any{change.NowMS},
		Scope: recordstore.Scope{
			Where: ownedItemPredicate + ` AND execution_state='COPYING'`,
			Args:  ownedItemArguments(change.Before),
		},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}

func (records itemWorkRecords) Finish(ctx context.Context, change application.ItemFinish) error {
	if err := records.fence(ctx, change.Before, change.NowMS); err != nil {
		return err
	}
	failure, matches, err := encodeItemOutcome(change.Outcome)
	if err != nil {
		return err
	}
	outcome := change.Outcome
	result, err := recordstore.UpdateEmulationstationImportItems(ctx, records.executor, recordstore.Update{
		Set: `execution_state=?,error_code=?,retryable=?,error_details_json=?,existing_game_id=COALESCE(?,existing_game_id),
existing_matches_json=COALESCE(?,existing_matches_json),completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Values: []any{
			outcome.State,
			optionalText(outcome.Code),
			outcome.Retryable,
			failure,
			optionalText(outcome.ExistingGameID),
			matches,
			change.NowMS,
			change.NowMS,
		},
		Scope: recordstore.Scope{
			Where: ownedItemPredicate + ` AND execution_state IN ('COPYING','VALIDATING')`,
			Args:  ownedItemArguments(change.Before),
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	if _, err := payloadrelease.ScheduleTerminalEmulationStationItem(
		ctx,
		records.transaction,
		change.Before.Item.ID,
		change.NowMS,
	); err != nil {
		return fmt.Errorf("schedule EmulationStation item payload: %w", err)
	}
	return refreshItemProgress(
		ctx,
		records.executor,
		change.Before.Execution,
		change.Before.Item.ID,
		outcome.State,
		change.NowMS,
	)
}

func encodeItemOutcome(outcome application.ItemOutcome) (*string, *string, error) {
	var failure, matches *string
	if outcome.Failure != nil {
		encoded, err := json.Marshal(outcome.Failure)
		if err != nil {
			return nil, nil, fmt.Errorf("encode EmulationStation item failure: %w", err)
		}
		value := string(encoded)
		failure = &value
	}
	if outcome.ExistingMatches != nil {
		encoded, err := json.Marshal(outcome.ExistingMatches)
		if err != nil {
			return nil, nil, fmt.Errorf("encode EmulationStation duplicate matches: %w", err)
		}
		value := string(encoded)
		matches = &value
	}
	return failure, matches, nil
}
