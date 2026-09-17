package emulationstationimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/emulationstationimport"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
)

func (service *ItemWork) Next(ctx context.Context, unit model.Execution) (model.ExecutionItem, bool, error) {
	var item model.ExecutionItem
	found := false
	err := service.repository.WithItemWork(ctx, func(scope model.ItemWorkScope) error {
		execution, err := currentExecution(ctx, scope.Read, unit)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		if err := validateImportExecution(execution, unit, now); err != nil {
			return err
		}
		item, found, err = scope.Read.Next(ctx, unit.ImportID)
		if err != nil {
			return fmt.Errorf("read next EmulationStation item: %w", err)
		}
		if !found {
			return nil
		}
		if item.ImportID != unit.ImportID || !validItemVersion(item.Version) || !workingItemState(item.State) {
			return model.ErrVersionConflict
		}
		if item.State != "PENDING" {
			return nil
		}
		if err := scope.Write.Claim(
			ctx,
			model.ItemClaim{Before: model.OwnedItem{Execution: execution, Item: item}, NowMS: now},
		); err != nil {
			return fmt.Errorf("claim EmulationStation item: %w", err)
		}
		item.State, item.Version = "COPYING", item.Version+1
		return nil
	})
	if err != nil {
		return model.ExecutionItem{}, false, fmt.Errorf("begin EmulationStation item work: %w", err)
	}
	return item, found, nil
}

func (service *ItemWork) Resume(ctx context.Context, unit model.Execution, itemID, jobID, ordinaryID string) error {
	err := service.repository.WithItemWork(ctx, func(scope model.ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, unit, itemID)
		if err != nil {
			return err
		}
		if jobID == "" ||
			ordinaryID == "" ||
			before.Item.LibraryImportJobID != jobID ||
			before.Item.LibraryImportItemID != ordinaryID {
			return model.ErrVersionConflict
		}
		if before.Item.State == "VALIDATING" || before.Item.State == "REVIEW_PENDING" {
			return nil
		}
		if before.Item.State != "COPYING" {
			return model.ErrVersionConflict
		}
		if err := scope.Write.Resume(ctx, model.ItemResume{Before: before, NowMS: now}); err != nil {
			return fmt.Errorf("resume EmulationStation review item: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("resume EmulationStation item: %w", err)
	}
	return nil
}

func (service *ItemWork) Finish(ctx context.Context, unit model.Execution, id string, outcome model.ItemOutcome) error {
	if !validItemOutcome(outcome) {
		return model.ErrInvalid
	}
	err := service.repository.WithItemWork(ctx, func(scope model.ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, unit, id)
		if err != nil {
			return err
		}
		if before.Item.State != "COPYING" && before.Item.State != "VALIDATING" {
			return model.ErrVersionConflict
		}
		if err := scope.Write.Finish(ctx, model.ItemFinish{Before: before, Outcome: outcome, NowMS: now}); err != nil {
			return fmt.Errorf("save EmulationStation item outcome: %w", err)
		}
		_, err = payloadreleaseservice.NewScheduler(nil).TerminalSource(
			ctx, scope.Payload.Scheduling,
			payloadreleasemodel.Scope{Type: payloadreleasemodel.ScopeEmulationStationImportItem, ID: id}, now,
		)
		if err != nil {
			return fmt.Errorf("schedule EmulationStation item payloads: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish EmulationStation item: %w", err)
	}
	return nil
}

func (service *ItemWork) owned(
	ctx context.Context,
	reader model.ItemWorkReader,
	unit model.Execution,
	id string,
) (model.OwnedItem, int64, error) {
	before, err := reader.Item(ctx, id)
	if err != nil {
		return model.OwnedItem{}, 0, fmt.Errorf("read EmulationStation item ownership: %w", err)
	}
	now := service.now().UnixMilli()
	if err := validateImportExecution(before.Execution, unit, now); err != nil {
		return model.OwnedItem{}, 0, err
	}
	if before.Item.ID != id || before.Item.ImportID != unit.ImportID || !validItemVersion(before.Item.Version) {
		return model.OwnedItem{}, 0, model.ErrVersionConflict
	}
	return before, now, nil
}
