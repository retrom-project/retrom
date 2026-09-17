package runtimeprovider

import (
	"context"
	"fmt"
	"time"
)

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }
func (service *Service) Reconcile(ctx context.Context, candidate Projection, now time.Time) error {
	if len(candidate.CatalogSHA256) != 64 || len(candidate.Providers) == 0 || now.UnixMilli() < 0 {
		return ErrProjectionInvalid
	}
	err := service.repository.CommitReconcile(ctx, ReconcileCommand{
		Candidate: candidate, NowMS: now.UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: %w", err)
	}
	return nil
}
