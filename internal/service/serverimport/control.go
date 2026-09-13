package serverimport

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Control struct {
	repository ControlRepository
	roots      map[string]string
	now        func() time.Time
}

func NewControl(repository ControlRepository, roots map[string]string, now func() time.Time) *Control {
	return &Control{repository: repository, roots: maps.Clone(roots), now: now}
}

func (service *Control) Cancel(
	ctx context.Context,
	id string,
	version int64,
	reason, actorID string,
) (Summary, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Summary{}, false, ErrNotCancellable
	}
	var result Summary
	var pending bool
	err := service.repository.WithWrite(ctx, func(scope ControlScope) error {
		before, err := scope.Read.Current(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return ErrNotCancellable
		}
		if err != nil {
			return fmt.Errorf("read import cancellation state: %w", err)
		}
		if before.Summary.Version != version || version == math.MaxInt64 ||
			before.JobState != before.Summary.State ||
			(before.Summary.State != "QUEUED" && before.Summary.State != "RUNNING") {
			return ErrNotCancellable
		}
		evidence, err := newControlEvidence(actorID, service.now().UnixMilli(), []byte(`{"schemaVersion":1}`))
		if err != nil {
			return err
		}
		plan := Cancellation{
			Before:         before,
			Pending:        before.Summary.State == "RUNNING",
			State:          "CANCEL_REQUESTED",
			Reason:         reason,
			CancelledItems: before.Summary.Counts.Cancelled,
			Evidence:       evidence,
		}
		if !plan.Pending {
			plan.State = "CANCELLED"
			now := evidence.Now
			plan.CompletedAt = &now
			plan.CancelledItems += before.PendingItems
		}
		if err := scope.Write.Cancel(ctx, plan); err != nil {
			return fmt.Errorf("cancel server import: %w", err)
		}
		after, err := scope.Read.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read cancelled import: %w", err)
		}
		result = after.Summary
		pending = plan.Pending
		return nil
	})
	if err != nil {
		return Summary{}, false, fmt.Errorf("commit server import cancellation: %w", err)
	}
	return result, pending, nil
}

func (service *Control) Retry(ctx context.Context, id string, version int64, actorID string) (Summary, error) {
	var result Summary
	err := service.repository.WithWrite(ctx, func(scope ControlScope) error {
		before, err := scope.Read.Current(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return ErrNotRetryable
		}
		if err != nil {
			return fmt.Errorf("read import retry state: %w", err)
		}
		if !service.retryable(before, version) {
			return ErrNotRetryable
		}
		plan, err := newManualRetry(before, actorID, service.now().UnixMilli())
		if err != nil {
			return err
		}
		if err := scope.Write.Retry(ctx, plan); err != nil {
			return fmt.Errorf("reset server import: %w", err)
		}
		after, err := scope.Read.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read retried import: %w", err)
		}
		result = after.Summary
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("commit server import retry: %w", err)
	}
	return result, nil
}

func (service *Control) retryable(before ControlSnapshot, version int64) bool {
	summary := before.Summary
	if before.OtherActive || summary.Version != version || version == math.MaxInt64 ||
		before.Execution < 1 || before.Execution == math.MaxInt64 ||
		summary.State != "FAILED" || before.JobState != "FAILED" || summary.LastErrorCode == nil {
		return false
	}
	if *summary.LastErrorCode != "SERVER_IMPORT_ROOT_UNAVAILABLE" && *summary.LastErrorCode != "INTERNAL_ERROR" {
		return false
	}
	digest, found := service.roots[summary.Root.ID]
	return found && digest == before.RootDigest
}

func newControlEvidence(actorID string, now int64, event []byte) (ControlEvidence, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return ControlEvidence{}, fmt.Errorf("create import audit identity: %w", err)
	}
	return ControlEvidence{ActorID: actorID, AuditID: id.String(), Event: event, Now: now}, nil
}
