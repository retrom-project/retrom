package emulationstationimport

import (
	"context"
	"database/sql"
	"fmt"

	payloadmodel "retrom/internal/model/payloadrelease"
	payloadrepo "retrom/internal/repo/payloadrelease"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
)

type ItemWork struct{ database *sql.DB }

func NewItemWork(database *sql.DB) *ItemWork { return &ItemWork{database: database} }

func (repository *ItemWork) ClaimNextItem(ctx context.Context, unit application.Execution, nowMS int64) (application.ClaimNextItemResult, error) {
	var result application.ClaimNextItemResult
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := itemWorkRecords{executor: executor}
		execution, found, err := records.Current(ctx, unit.JobID)
		if err != nil {
			return fmt.Errorf("read current EmulationStation execution: %w", err)
		}
		if !found || execution.Execution != unit {
			return application.ErrVersionConflict
		}
		if err := application.ValidateImportExecution(execution, unit, nowMS); err != nil {
			return err
		}
		item, found, err := records.Next(ctx, unit.ImportID)
		if err != nil {
			return fmt.Errorf("read next EmulationStation item: %w", err)
		}
		if !found {
			return nil
		}
		result.Found = true
		if item.ImportID != unit.ImportID || !application.ValidItemVersion(item.Version) || !application.WorkingItemState(item.State) {
			return application.ErrVersionConflict
		}
		if item.State != "PENDING" {
			result.Item = item
			return nil
		}
		if err := records.Claim(ctx, application.ItemClaim{Before: application.OwnedItem{Execution: execution, Item: item}, NowMS: nowMS}); err != nil {
			return fmt.Errorf("claim EmulationStation item: %w", err)
		}
		item.State, item.Version = "COPYING", item.Version+1
		result.Item = item
		return nil
	})
	if err != nil {
		return application.ClaimNextItemResult{}, err
	}
	return result, nil
}

func (repository *ItemWork) CommitItemResume(ctx context.Context, unit application.Execution, itemID, jobID, ordinaryID string, nowMS int64) error {
	return dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := itemWorkRecords{executor: executor}
		before, err := records.Item(ctx, itemID)
		if err != nil {
			return fmt.Errorf("read EmulationStation item ownership: %w", err)
		}
		if err := application.ValidateImportExecution(before.Execution, unit, nowMS); err != nil {
			return err
		}
		if before.Item.ID != itemID || before.Item.ImportID != unit.ImportID || !application.ValidItemVersion(before.Item.Version) {
			return application.ErrVersionConflict
		}
		if jobID == "" || ordinaryID == "" ||
			before.Item.LibraryImportJobID != jobID ||
			before.Item.LibraryImportItemID != ordinaryID {
			return application.ErrVersionConflict
		}
		if before.Item.State == "VALIDATING" || before.Item.State == "REVIEW_PENDING" {
			return nil
		}
		if before.Item.State != "COPYING" {
			return application.ErrVersionConflict
		}
		if err := records.Resume(ctx, application.ItemResume{Before: before, NowMS: nowMS}); err != nil {
			return fmt.Errorf("resume EmulationStation review item: %w", err)
		}
		return nil
	})
}

func (repository *ItemWork) CommitItemFinish(ctx context.Context, unit application.Execution, itemID string, outcome application.ItemOutcome, nowMS int64) error {
	return dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := itemWorkRecords{executor: executor}
		before, err := records.Item(ctx, itemID)
		if err != nil {
			return fmt.Errorf("read EmulationStation item ownership: %w", err)
		}
		if err := application.ValidateImportExecution(before.Execution, unit, nowMS); err != nil {
			return err
		}
		if before.Item.ID != itemID || before.Item.ImportID != unit.ImportID || !application.ValidItemVersion(before.Item.Version) {
			return application.ErrVersionConflict
		}
		if before.Item.State != "COPYING" && before.Item.State != "VALIDATING" {
			return application.ErrVersionConflict
		}
		if err := records.Finish(ctx, application.ItemFinish{Before: before, Outcome: outcome, NowMS: nowMS}); err != nil {
			return fmt.Errorf("save EmulationStation item outcome: %w", err)
		}
		scope := payloadrepo.BindReleases(executor)
		_, err = payloadrepo.NewScheduler(nil).TerminalSource(
			ctx, scope.Scheduling,
			payloadmodel.Scope{Type: payloadmodel.ScopeEmulationStationImportItem, ID: itemID}, nowMS,
		)
		if err != nil {
			return fmt.Errorf("schedule EmulationStation item payloads: %w", err)
		}
		return nil
	})
}

type itemWorkRecords struct {
	executor dbexec.Executor
}

func (records itemWorkRecords) Current(ctx context.Context, id string) (application.LeaseSnapshot, bool, error) {
	return (leaseRecords{executor: records.executor}).Current(ctx, id)
}

func (records itemWorkRecords) fence(ctx context.Context, before application.OwnedItem, now int64) error {
	return (executionRecords{executor: records.executor}).Fence(ctx, before.Execution, now)
}

const ownedItemPredicate = `id=? AND import_id=? AND version=? AND execution_state=?
AND metadata_json=? AND content_kind=? AND library_import_job_id IS ? AND library_import_item_id IS ?`

func ownedItemArguments(before application.OwnedItem) []any {
	item := before.Item
	return []any{
		item.ID,
		item.ImportID,
		item.Version,
		item.State,
		item.MetadataJSON,
		item.ContentKind,
		optionalText(item.LibraryImportJobID),
		optionalText(item.LibraryImportItemID),
	}
}
