package libraryimport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"retrom/internal/authn"

	"github.com/google/uuid"
)

type ReviewBulk struct {
	repository ReviewBulkStore
	approvals  *ReviewApprovals
	now        func() time.Time
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	started    bool
	workers    sync.WaitGroup
}

func NewReviewBulk(repository ReviewBulkStore, approvals *ReviewApprovals, now func() time.Time) *ReviewBulk {
	ctx, cancel := context.WithCancel(context.Background())
	return &ReviewBulk{repository: repository, approvals: approvals, now: now, ctx: ctx, cancel: cancel}
}

func (service *ReviewBulk) launch(id string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed {
		return
	}
	service.launchLocked(id)
}

func (service *ReviewBulk) launchLocked(id string) {
	service.workers.Add(1)
	go func() {
		defer service.workers.Done()
		service.runReviewBulkApproval(service.ctx, id)
	}()
}

func (service *ReviewBulk) Close() {
	service.mu.Lock()
	service.closed = true
	service.cancel()
	service.mu.Unlock()
	service.workers.Wait()
}

func (service *ReviewBulk) Create(ctx context.Context) (ReviewBulkSummary, error) {
	service.mu.Lock()
	closed := service.closed
	service.mu.Unlock()
	if closed {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	principal, ok := authn.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	bulkID, err := uuid.NewV7()
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("allocate bulk approval: %w", err)
	}
	jobID, err := uuid.NewV7()
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("allocate bulk job: %w", err)
	}
	now := service.now().UnixMilli()
	var created ReviewBulkSummary
	err = service.repository.WithStep(ctx, func(scope ReviewBulkStep) error {
		repository := scope.Writes
		var createErr error
		created, createErr = repository.CreateGlobal(ctx, bulkID.String(), jobID.String(), principal.UserID, now)
		if createErr != nil {
			return fmt.Errorf("create bounded global review: %w", createErr)
		}
		return nil
	})
	if err != nil {
		_, active, readErr := service.repository.ActiveSummary(ctx)
		if readErr == nil && active {
			return ReviewBulkSummary{}, ErrReviewBulkActive
		}
		if errors.Is(err, ErrReviewBulkEmpty) {
			return ReviewBulkSummary{}, ErrReviewBulkEmpty
		}
		if errors.Is(err, ErrReviewBulkTooLarge) {
			return ReviewBulkSummary{}, ErrReviewBulkTooLarge
		}
		return ReviewBulkSummary{}, fmt.Errorf("create review bulk: %w", err)
	}
	service.launch(created.BulkApprovalID)
	return created, nil
}

func (service *ReviewBulk) Get(ctx context.Context, bulkID string) (ReviewBulkSummary, error) {
	if _, err := uuid.Parse(bulkID); err != nil {
		return ReviewBulkSummary{}, ErrReviewBulkConflict
	}
	summary, err := service.repository.Summary(ctx, bulkID)
	if err != nil {
		return ReviewBulkSummary{}, fmt.Errorf("read review bulk: %w", err)
	}
	return summary, nil
}

func (service *ReviewBulk) Active(ctx context.Context) (ReviewBulkSummary, bool, error) {
	result, found, err := service.repository.ActiveSummary(ctx)
	if err != nil {
		return ReviewBulkSummary{}, false, fmt.Errorf("read active review bulk: %w", err)
	}
	return result, found, nil
}
