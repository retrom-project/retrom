package filestaging

import (
	"context"
	"fmt"
	"sync"
	"time"

	"retrom/internal/filestore"
)

type Repository interface {
	Registered(context.Context, string) (bool, error)
	Retire(context.Context, int64, int64) error
}
type Service struct {
	repository Repository
	files      *filestore.Store
	now        func() time.Time
	mutex      sync.Mutex
	next       time.Time
}

func New(repository Repository, files *filestore.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, files: files, now: now}
}

func (service *Service) Run(ctx context.Context) error {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	now := service.now()
	if now.Before(service.next) {
		return nil
	}
	cutoff := now.Add(-filestore.UnregisteredLifetime)
	if err := service.repository.Retire(ctx, cutoff.UnixMilli(), now.UnixMilli()); err != nil {
		return fmt.Errorf("service: %w", err)
	}
	if err := service.files.Sweep(ctx, cutoff, service.repository.Registered); err != nil {
		return fmt.Errorf("service: %w", err)
	}
	service.next = now.Add(time.Hour)
	return nil
}
