package serverimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"
)

var ErrOutcomeIncomplete = errors.New("SERVER_IMPORT_ITEMS_UNFINISHED")

type Outcomes struct {
	repository OutcomeRepository
	now        func() time.Time
}

func NewOutcomes(repository OutcomeRepository, now func() time.Time) *Outcomes {
	return &Outcomes{repository, now}
}

func (service *Outcomes) Finish(ctx context.Context, unit Work) error {
	err := service.repository.WithWrite(ctx, func(scope OutcomeScope) error {
		now := service.now().UnixMilli()
		counts, err := lockedCounts(ctx, scope, unit, now, RunningWorker)
		if err != nil {
			return err
		}
		if counts["PENDING"]+counts["EVALUATING"] > 0 {
			return ErrOutcomeIncomplete
		}
		totals := terminalCounts(counts)
		state := "COMPLETED"
		if totals.Failed > 0 {
			state = "PARTIAL_FAILURE"
		}
		phase := "QUEUEING_REVALIDATION"
		return writeFinal(
			ctx,
			scope.Write,
			FinalOutcome{
				Unit:        unit,
				State:       state,
				JobState:    "SUCCEEDED",
				EventType:   "SUCCEEDED",
				Phase:       &phase,
				HeartbeatAt: &now,
				Counts:      totals,
				Event: []byte(
					`{"schemaVersion":1}`,
				),
				Now: now,
			},
		)
	})
	if err != nil {
		return fmt.Errorf("finish server import: %w", err)
	}
	return nil
}

func (service *Outcomes) Cancel(ctx context.Context, unit Work) error {
	err := service.repository.WithWrite(ctx, func(scope OutcomeScope) error {
		now := service.now().UnixMilli()
		counts, err := lockedCounts(ctx, scope, unit, now, CancelledWorker)
		if err != nil {
			return err
		}
		counts = movePending(counts, "CANCELLED")
		return writeFinal(
			ctx,
			scope.Write,
			FinalOutcome{
				Unit:         unit,
				State:        "CANCELLED",
				JobState:     "CANCELLED",
				EventType:    "CANCELLED",
				PendingState: "CANCELLED",
				PendingCode:  "CANCELLED",
				Counts: terminalCounts(
					counts,
				),
				Event: []byte(
					`{"schemaVersion":1}`,
				),
				Now: now,
			},
		)
	})
	if err != nil {
		return fmt.Errorf("cancel import execution: %w", err)
	}
	return nil
}

func (service *Outcomes) Fail(ctx context.Context, unit Work, code string) (int64, error) {
	var retryAt int64
	err := service.repository.WithWrite(ctx, func(scope OutcomeScope) error {
		now := service.now().UnixMilli()
		access := RunningWorker
		if unit.Recovery {
			access = ExhaustedWorker
		}
		counts, err := lockedCounts(ctx, scope, unit, now, access)
		if err != nil {
			return err
		}
		retryable := code == "SERVER_IMPORT_ROOT_UNAVAILABLE" || code == "INTERNAL_ERROR"
		if retryable && !unit.Recovery {
			retryAt, err = retryExecution(ctx, scope, unit, counts, code, now)
			if err != nil || retryAt != 0 {
				return err
			}
		}
		event, err := json.Marshal(map[string]any{"schemaVersion": 1, "code": code})
		if err != nil {
			return fmt.Errorf("encode import failure: %w", err)
		}
		counts = movePending(counts, "COMMIT_FAILED")
		return writeFinal(
			ctx,
			scope.Write,
			FinalOutcome{
				Unit:         unit,
				State:        "FAILED",
				JobState:     "FAILED",
				EventType:    "FAILED",
				Code:         &code,
				Retryable:    &retryable,
				PendingState: "COMMIT_FAILED",
				PendingCode:  code,
				Counts: terminalCounts(
					counts,
				),
				Event: event,
				Now:   now,
			},
		)
	})
	if err != nil {
		return 0, fmt.Errorf("fail import execution: %w", err)
	}
	return retryAt, nil
}

func (service *Outcomes) Reconcile(ctx context.Context) (bool, error) {
	pending, found, err := service.repository.Recovery(ctx, service.now().UnixMilli())
	if err != nil {
		return false, fmt.Errorf("find import recovery: %w", err)
	}
	if !found {
		return false, nil
	}
	if pending.Cancelled {
		if err := service.Cancel(ctx, pending.Unit); err != nil {
			return false, err
		}
	} else {
		if _, err := service.Fail(ctx, pending.Unit, "INTERNAL_ERROR"); err != nil {
			return false, err
		}
	}
	return true, nil
}

func lockedCounts(
	ctx context.Context,
	scope OutcomeScope,
	unit Work,
	now int64,
	access WorkerAccess,
) (map[string]int64, error) {
	if err := scope.Write.Lock(ctx, unit, now, access); err != nil {
		return nil, fmt.Errorf("lock import outcome: %w", err)
	}
	counts, err := scope.Read.Counts(ctx, unit)
	if err != nil {
		return nil, fmt.Errorf("read import outcome counts: %w", err)
	}
	return counts, nil
}

func writeFinal(ctx context.Context, writer OutcomeWriter, plan FinalOutcome) error {
	if err := writer.Final(ctx, plan); err != nil {
		return fmt.Errorf("write terminal import: %w", err)
	}
	return nil
}

func movePending(counts map[string]int64, state string) map[string]int64 {
	result := maps.Clone(counts)
	result[state] += result["PENDING"] + result["EVALUATING"]
	delete(result, "PENDING")
	delete(result, "EVALUATING")
	return result
}

func terminalCounts(counts map[string]int64) TerminalCounts {
	return TerminalCounts{
		Matched:          counts["IMPORTED_MATCHED"],
		Warning:          counts["IMPORTED_WARNING"],
		Missing:          counts["IMPORTED_MISSING_ENTRY"],
		NotFound:         counts["NOT_FOUND"],
		SkippedExisting:  counts["SKIPPED_EXISTING"],
		SkippedNotBetter: counts["SKIPPED_NOT_BETTER"],
		SameBytes:        counts["ALREADY_SAME_BYTES"],
		Failed: counts["SOURCE_CHANGED"] + counts["CATALOG_CHANGED"] +
			counts["READ_FAILED"] + counts["INVALID_ARCHIVE"] + counts["COMMIT_FAILED"],
		Cancelled: counts["CANCELLED"],
	}
}
