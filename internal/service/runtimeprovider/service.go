package runtimeprovider

import (
	"context"
	"fmt"
	model "retrom/internal/model/runtimeprovider"
	"time"
)

type Service struct{ repository model.Repository }

func New(repository model.Repository) *Service { return &Service{repository: repository} }
func (service *Service) Reconcile(ctx context.Context, candidate model.Projection, now time.Time) error {
	if len(candidate.CatalogSHA256) != 64 || len(candidate.Providers) == 0 || now.UnixMilli() < 0 {
		return model.ErrProjectionInvalid
	}
	err := service.repository.CommitReconcile(ctx, model.ReconcileCommand{
		Candidate: candidate, NowMS: now.UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: %w", err)
	}
	return nil
}
