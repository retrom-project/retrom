package pegasusimport

import (
	"context"
	"fmt"
	"math"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	model "retrom/internal/model/pegasusimport"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
	"time"
)

type ItemWork struct {
	repository model.ItemWorkRepository
	now        func() time.Time
}

func NewItemWork(repository model.ItemWorkRepository, now func() time.Time) *ItemWork {
	return &ItemWork{repository: repository, now: now}
}

func (service *ItemWork) Next(ctx context.Context, identity model.ExecutionIdentity) (model.ExecutionItem, bool, error) {
	var item model.ExecutionItem
	found := false
	err := service.repository.WithItemWork(ctx, func(scope model.ItemWorkScope) error {
		execution, err := scope.Read.Execution(ctx, identity.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus item execution: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(execution, identity, now); err != nil {
			return err
		}
		if execution.Kind != "SERVER_PEGASUS_IMPORT" || execution.JobState != "RUNNING" {
			return model.ErrVersionConflict
		}
		item, found, err = scope.Read.Next(ctx, identity.ImportID)
		if err != nil {
			return fmt.Errorf("read next Pegasus item: %w", err)
		}
		if !found {
			return nil
		}
		if item.ImportID != identity.ImportID || item.State != "PENDING" || !validItemVersion(item.Version) {
			return model.ErrVersionConflict
		}
		if err := scope.Write.Claim(
			ctx,
			model.ItemClaim{Before: model.OwnedItem{Execution: execution, Item: item}, NowMS: now},
		); err != nil {
			return fmt.Errorf("claim Pegasus item: %w", err)
		}
		item.State, item.Version = "COPYING", item.Version+1
		return nil
	})
	if err != nil {
		return model.ExecutionItem{}, false, fmt.Errorf("begin Pegasus item work: %w", err)
	}
	return item, found, nil
}

func (service *ItemWork) Resume(
	ctx context.Context,
	identity model.ExecutionIdentity,
	itemID, jobID, libraryItemID string,
) error {
	err := service.repository.WithItemWork(ctx, func(scope model.ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, identity, itemID)
		if err != nil {
			return err
		}
		if jobID == "" || libraryItemID == "" || before.Item.LibraryImportJobID != jobID ||
			before.Item.LibraryImportItemID != libraryItemID {
			return model.ErrVersionConflict
		}
		if before.Item.State == "VALIDATING" || before.Item.State == "REVIEW_PENDING" {
			return nil
		}
		if before.Item.State != "COPYING" {
			return model.ErrVersionConflict
		}
		if err := scope.Write.Resume(ctx, model.ItemResume{Before: before, NowMS: now}); err != nil {
			return fmt.Errorf("resume bound Pegasus review: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("resume Pegasus item: %w", err)
	}
	return nil
}

func (service *ItemWork) Finish(
	ctx context.Context,
	identity model.ExecutionIdentity,
	itemID string,
	outcome model.ItemOutcome,
) error {
	if !validItemOutcome(outcome) {
		return model.ErrInvalid
	}
	err := service.repository.WithItemWork(ctx, func(scope model.ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, identity, itemID)
		if err != nil {
			return err
		}
		if before.Item.State == outcome.State {
			return nil
		}
		if before.Item.State != "COPYING" && before.Item.State != "VALIDATING" {
			return model.ErrVersionConflict
		}
		if err := scope.Write.Finish(ctx, model.ItemFinish{Before: before, Outcome: outcome, NowMS: now}); err != nil {
			return fmt.Errorf("save Pegasus item outcome: %w", err)
		}
		_, err = payloadreleaseservice.NewScheduler(nil).TerminalSource(
			ctx, scope.Payload.Scheduling,
			payloadreleasemodel.Scope{Type: payloadreleasemodel.ScopePegasusImportItem, ID: itemID}, now,
		)
		if err != nil {
			return fmt.Errorf("schedule Pegasus item payloads: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish Pegasus item: %w", err)
	}
	return nil
}

func (service *ItemWork) owned(
	ctx context.Context,
	reader model.ItemWorkReader,
	identity model.ExecutionIdentity,
	id string,
) (model.OwnedItem, int64, error) {
	before, err := reader.Current(ctx, id)
	if err != nil {
		return model.OwnedItem{}, 0, fmt.Errorf("read Pegasus item ownership: %w", err)
	}
	now := service.now().UnixMilli()
	if err := ValidateExecution(before.Execution, identity, now); err != nil {
		return model.OwnedItem{}, 0, err
	}
	if before.Item.ID != id || before.Item.ImportID != identity.ImportID || !validItemVersion(before.Item.Version) {
		return model.OwnedItem{}, 0, model.ErrVersionConflict
	}
	return before, now, nil
}

func validItemVersion(version int64) bool { return version > 0 && version < math.MaxInt64 }

func validItemOutcome(outcome model.ItemOutcome) bool {
	switch outcome.State {
	case "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED", "BLOCKED_CONTENT", "CANCELLED":
		return outcome.Code != ""
	case "SKIPPED_EXISTING":
		return outcome.ExistingGameID != "" && len(outcome.ExistingMatches) > 0
	default:
		return false
	}
}
