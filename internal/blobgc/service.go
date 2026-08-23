package blobgc

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"retrom/internal/blobregistry"
	"retrom/internal/blobstore"
	"retrom/internal/payloadrelease"
)

type Result struct {
	Protected int `json:"protected"`
	Scheduled int `json:"scheduled"`
	Deleted   int `json:"deleted"`
	Retained  int `json:"retained"`
}

type Service struct {
	database *sql.DB
	release  *payloadrelease.Service
}

func New(database *sql.DB, blobs *blobstore.Store, now func() time.Time, retention time.Duration) (*Service, error) {
	release, err := payloadrelease.New(database, blobs, now, retention)
	if err != nil {
		return nil, fmt.Errorf("blobgc/create release service: %w", err)
	}
	return &Service{database: database, release: release}, nil
}

// RunOnce remains as the deterministic maintenance seam; production uses the
// same payload-release dispatcher continuously.
func (service *Service) RunOnce(ctx context.Context) (Result, error) {
	beforeBlobs, beforeCandidates, err := service.counts(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := service.release.ReconcileGC(ctx); err != nil {
		return Result{}, fmt.Errorf("blobgc/reconcile: %w", err)
	}
	_, _ = service.release.RunOnce(ctx)
	afterBlobs, afterCandidates, err := service.counts(ctx)
	if err != nil {
		return Result{}, err
	}
	protected, err := blobregistry.ProtectiveSet(ctx, service.database)
	if err != nil {
		return Result{}, fmt.Errorf("blobgc/protection: %w", err)
	}
	result := Result{Protected: len(protected)}
	if afterCandidates > beforeCandidates {
		result.Scheduled = afterCandidates - beforeCandidates
	}
	if beforeBlobs > afterBlobs {
		result.Deleted = beforeBlobs - afterBlobs
	}
	result.Retained = afterBlobs - result.Protected
	return result, nil
}

func (service *Service) counts(ctx context.Context) (int, int, error) {
	var blobs, candidates int
	if err := service.database.QueryRowContext(ctx, `SELECT count(*) FROM blobs`).Scan(&blobs); err != nil {
		return 0, 0, fmt.Errorf("blobgc/count blobs: %w", err)
	}
	if err := service.database.QueryRowContext(
		ctx, `SELECT count(*) FROM blob_gc_candidates`,
	).Scan(&candidates); err != nil {
		return 0, 0, fmt.Errorf("blobgc/count candidates: %w", err)
	}
	return blobs, candidates, nil
}
