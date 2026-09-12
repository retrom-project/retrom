package blobgc

import (
	"context"
	"fmt"
)

type Result struct {
	Protected int `json:"protected"`
	Scheduled int `json:"scheduled"`
	Deleted   int `json:"deleted"`
	Retained  int `json:"retained"`
}

type Repository interface {
	Counts(context.Context) (int, int, error)
	Protected(context.Context) (int, error)
}

type Release interface {
	ReconcileGC(context.Context) error
	RunOnce(context.Context) (bool, error)
}

type Service struct {
	repository Repository
	release    Release
}

func New(repository Repository, release Release) *Service {
	return &Service{repository: repository, release: release}
}

// RunOnce remains as the deterministic maintenance seam; production uses the
// same payload-release dispatcher continuously.
func (service *Service) RunOnce(ctx context.Context) (Result, error) {
	beforeBlobs, beforeCandidates, err := service.repository.Counts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("blobgc/count before release: %w", err)
	}
	if err := service.release.ReconcileGC(ctx); err != nil {
		return Result{}, fmt.Errorf("blobgc/reconcile: %w", err)
	}
	_, _ = service.release.RunOnce(ctx)
	afterBlobs, afterCandidates, err := service.repository.Counts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("blobgc/count after release: %w", err)
	}
	protected, err := service.repository.Protected(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("blobgc/protection: %w", err)
	}
	result := Result{Protected: protected}
	if afterCandidates > beforeCandidates {
		result.Scheduled = afterCandidates - beforeCandidates
	}
	if beforeBlobs > afterBlobs {
		result.Deleted = beforeBlobs - afterBlobs
	}
	result.Retained = afterBlobs - result.Protected
	return result, nil
}
