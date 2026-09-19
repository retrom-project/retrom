package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type Recovery struct {
	repository model.RecoveryRepository
	now        func() time.Time
}

func NewRecovery(repository model.RecoveryRepository, now func() time.Time) *Recovery {
	return &Recovery{repository: repository, now: now}
}

func (service *Recovery) Recover(ctx context.Context) error {
	candidates, err := service.repository.Expired(ctx, service.now().UnixMilli(), 100)
	if err != nil {
		return fmt.Errorf("list expired EmulationStation executions: %w", err)
	}
	for _, candidate := range candidates {
		if err := service.recoverCandidate(ctx, candidate); err != nil {
			if errors.Is(err, model.ErrVersionConflict) {
				continue
			}
			return fmt.Errorf("recover EmulationStation execution %s: %w", candidate.JobID, err)
		}
	}
	return nil
}

func (service *Recovery) recoverCandidate(ctx context.Context, candidate model.LeaseSnapshot) error {
	current, found, err := service.repository.CurrentRecovery(ctx, candidate.JobID)
	if err != nil {
		return fmt.Errorf("read EmulationStation recovery candidate: %w", err)
	}
	if !found {
		return nil
	}
	if !sameRecoverySnapshot(current, candidate) {
		return model.ErrVersionConflict
	}
	if _, err := planRecovery(current, service.now().UnixMilli()); err != nil {
		return err
	}

	now := service.now()
	batch, err := service.repository.CommitRecoveryReviewBatch(ctx, current, now.UnixMilli(), now.UTC().Year()+1)
	if err != nil {
		return err
	}
	if !batch.Found {
		return nil
	}
	current = batch.Before
	if batch.More {
		return nil
	}

	change, err := planRecovery(current, service.now().UnixMilli())
	if err != nil {
		return err
	}
	return service.repository.CommitRecovery(ctx, change)
}

func sameRecoverySnapshot(a, b model.LeaseSnapshot) bool {
	if (a.StartedAtMS == nil) != (b.StartedAtMS == nil) {
		return false
	}
	if a.StartedAtMS != nil && *a.StartedAtMS != *b.StartedAtMS {
		return false
	}
	a.StartedAtMS, b.StartedAtMS = nil, nil
	return a == b
}
