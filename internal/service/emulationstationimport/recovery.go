package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type (
	RecoveryChange struct {
		Before                                               LeaseSnapshot
		JobState, ImportState, Phase, ItemState, Code, Event string
		NowMS, AvailableAtMS                                 int64
	}
	RecoveryReader interface {
		Current(context.Context, string) (LeaseSnapshot, bool, error)
	}
	RecoveryWriter interface {
		Apply(context.Context, RecoveryChange) error
	}
	RecoveryScope struct {
		Read  RecoveryReader
		Write RecoveryWriter
	}
	RecoveryRepository interface {
		Expired(context.Context, int64, int) ([]LeaseSnapshot, error)
		WithRecovery(context.Context, func(RecoveryScope) error) error
	}
	Recovery struct {
		repository RecoveryRepository
		now        func() time.Time
	}
)

func NewRecovery(repository RecoveryRepository, now func() time.Time) *Recovery {
	return &Recovery{repository: repository, now: now}
}

func (service *Recovery) Recover(ctx context.Context) error {
	candidates, err := service.repository.Expired(ctx, service.now().UnixMilli(), 100)
	if err != nil {
		return fmt.Errorf("list expired EmulationStation executions: %w", err)
	}
	for _, candidate := range candidates {
		err := service.repository.WithRecovery(ctx, func(scope RecoveryScope) error {
			current, found, err := scope.Read.Current(ctx, candidate.JobID)
			if err != nil {
				return fmt.Errorf("read EmulationStation recovery candidate: %w", err)
			}
			if !found {
				return nil
			}
			if !sameRecoverySnapshot(current, candidate) {
				return ErrVersionConflict
			}
			change, err := planRecovery(current, service.now().UnixMilli())
			if err != nil {
				return err
			}
			if err := scope.Write.Apply(ctx, change); err != nil {
				return fmt.Errorf("persist EmulationStation recovery: %w", err)
			}
			return nil
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("recover EmulationStation execution %s: %w", candidate.JobID, err)
		}
	}
	return nil
}

func sameRecoverySnapshot(a, b LeaseSnapshot) bool {
	if (a.StartedAtMS == nil) != (b.StartedAtMS == nil) {
		return false
	}
	if a.StartedAtMS != nil && *a.StartedAtMS != *b.StartedAtMS {
		return false
	}
	a.StartedAtMS, b.StartedAtMS = nil, nil
	return a == b
}
