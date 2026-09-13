package emulationstationimport

import (
	"context"
	"fmt"
	"math"
	"time"

	payload "retrom/internal/service/payloadrelease"
)

type CompletionCounts struct {
	Terminal                                                  TerminalItemCounts
	ExpectedItems, Unfinished, RetryableFailed, MediaWarnings int64
}
type CompletionChange struct {
	Before      LeaseSnapshot
	Counts      CompletionCounts
	ImportState string
	Retryable   bool
	NowMS       int64
}
type CompletionReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
	Counts(context.Context, string) (CompletionCounts, error)
}
type CompletionWriter interface {
	Complete(context.Context, CompletionChange) error
}
type CompletionScope struct {
	Payload payload.ReleaseScope
	Read    CompletionReader
	Write   CompletionWriter
}
type CompletionRepository interface {
	WithCompletion(context.Context, func(CompletionScope) error) error
}
type Completion struct {
	repository CompletionRepository
	now        func() time.Time
}

func NewCompletion(repository CompletionRepository, now func() time.Time) *Completion {
	return &Completion{repository: repository, now: now}
}

func (service *Completion) Finish(ctx context.Context, unit Execution) error {
	err := service.repository.WithCompletion(ctx, func(scope CompletionScope) error {
		before, err := currentExecution(ctx, scope.Read, unit)
		if err != nil {
			return err
		}
		now := service.now().UnixMilli()
		if err := validateImportExecution(before, unit, now); err != nil {
			return err
		}
		counts, err := scope.Read.Counts(ctx, unit.ImportID)
		if err != nil {
			return fmt.Errorf("read EmulationStation completion counts: %w", err)
		}
		change, err := planCompletion(before, counts, now)
		if err != nil {
			return err
		}
		if err := scope.Write.Complete(ctx, change); err != nil {
			return fmt.Errorf("persist EmulationStation completion: %w", err)
		}
		return scheduleTerminalPayloads(ctx, scope.Payload, change.Before.ImportID, change.NowMS)
	})
	if err != nil {
		return fmt.Errorf("complete EmulationStation import: %w", err)
	}
	return nil
}

func planCompletion(before LeaseSnapshot, counts CompletionCounts, now int64) (CompletionChange, error) {
	if counts.Unfinished > 0 {
		return CompletionChange{}, ErrActive
	}
	if !validCompletionCounts(counts) {
		return CompletionChange{}, ErrInvalid
	}
	state := "COMPLETED"
	if counts.Terminal.Failed > 0 || counts.Terminal.Blocked > 0 {
		state = "PARTIAL_FAILURE"
	}
	return CompletionChange{
		Before:      before,
		Counts:      counts,
		ImportState: state,
		Retryable:   counts.RetryableFailed > 0,
		NowMS:       now,
	}, nil
}

func validCompletionCounts(counts CompletionCounts) bool {
	if counts.ExpectedItems < 0 ||
		counts.Unfinished != 0 ||
		counts.RetryableFailed < 0 ||
		counts.RetryableFailed > counts.Terminal.Failed ||
		counts.MediaWarnings < 0 {
		return false
	}
	terminal := counts.Terminal
	sum := int64(0)
	for _, count := range []int64{
		terminal.SkippedMapping,
		terminal.ReviewPending,
		terminal.Published,
		terminal.ReviewDiscarded,
		terminal.Existing,
		terminal.Blocked,
		terminal.Failed,
		terminal.Cancelled,
	} {
		if count < 0 || sum > math.MaxInt64-count {
			return false
		}
		sum += count
	}
	return sum == counts.ExpectedItems
}
