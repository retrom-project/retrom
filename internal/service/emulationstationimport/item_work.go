package emulationstationimport

import (
	"context"
	"fmt"
	payload "retrom/internal/service/payloadrelease"
)

func (service *ItemWork) Next(ctx context.Context, unit Execution) (ExecutionItem, bool, error) {
	var item ExecutionItem
	found := false
	err := service.repository.WithItemWork(ctx, func(scope ItemWorkScope) error {
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
			return ErrVersionConflict
		}
		if item.State != "PENDING" {
			return nil
		}
		if err := scope.Write.Claim(
			ctx,
			ItemClaim{Before: OwnedItem{Execution: execution, Item: item}, NowMS: now},
		); err != nil {
			return fmt.Errorf("claim EmulationStation item: %w", err)
		}
		item.State, item.Version = "COPYING", item.Version+1
		return nil
	})
	if err != nil {
		return ExecutionItem{}, false, fmt.Errorf("begin EmulationStation item work: %w", err)
	}
	return item, found, nil
}

func (service *ItemWork) Resume(ctx context.Context, unit Execution, itemID, jobID, ordinaryID string) error {
	err := service.repository.WithItemWork(ctx, func(scope ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, unit, itemID)
		if err != nil {
			return err
		}
		if jobID == "" ||
			ordinaryID == "" ||
			before.Item.LibraryImportJobID != jobID ||
			before.Item.LibraryImportItemID != ordinaryID {
			return ErrVersionConflict
		}
		if before.Item.State == "VALIDATING" || before.Item.State == "REVIEW_PENDING" {
			return nil
		}
		if before.Item.State != "COPYING" {
			return ErrVersionConflict
		}
		if err := scope.Write.Resume(ctx, ItemResume{Before: before, NowMS: now}); err != nil {
			return fmt.Errorf("resume EmulationStation review item: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("resume EmulationStation item: %w", err)
	}
	return nil
}

func (service *ItemWork) Finish(ctx context.Context, unit Execution, id string, outcome ItemOutcome) error {
	if !validItemOutcome(outcome) {
		return ErrInvalid
	}
	err := service.repository.WithItemWork(ctx, func(scope ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, unit, id)
		if err != nil {
			return err
		}
		if before.Item.State != "COPYING" && before.Item.State != "VALIDATING" {
			return ErrVersionConflict
		}
		if err := scope.Write.Finish(ctx, ItemFinish{Before: before, Outcome: outcome, NowMS: now}); err != nil {
			return fmt.Errorf("save EmulationStation item outcome: %w", err)
		}
		_, err = payload.NewScheduler(nil).TerminalSource(ctx, scope.Payload.Scheduling, payload.Scope{Type: payload.ScopeEmulationStationImportItem, ID: id}, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("finish EmulationStation item: %w", err)
	}
	return nil
}

func (service *ItemWork) owned(
	ctx context.Context,
	reader ItemWorkReader,
	unit Execution,
	id string,
) (OwnedItem, int64, error) {
	before, err := reader.Item(ctx, id)
	if err != nil {
		return OwnedItem{}, 0, fmt.Errorf("read EmulationStation item ownership: %w", err)
	}
	now := service.now().UnixMilli()
	if err := validateImportExecution(before.Execution, unit, now); err != nil {
		return OwnedItem{}, 0, err
	}
	if before.Item.ID != id || before.Item.ImportID != unit.ImportID || !validItemVersion(before.Item.Version) {
		return OwnedItem{}, 0, ErrVersionConflict
	}
	return before, now, nil
}
