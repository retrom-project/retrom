package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/blobregistry"
	repository "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

type ImmediateGCResult = application.ImmediateGCResult

func (service *Service) ScheduleImmediateGC(ctx context.Context, actor string) (ImmediateGCResult, error) {
	result, err := service.gc.Immediate(ctx, actor)
	if err != nil {
		return ImmediateGCResult{}, fmt.Errorf("schedule immediate GC: %w", err)
	}
	return result, nil
}

func (service *Service) stageAllUnreferenced(ctx context.Context) error {
	if err := service.gc.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile GC candidates: %w", err)
	}
	return nil
}

func (service *Service) stageCandidates(ctx context.Context, tx *sql.Tx, ids []string) error {
	if err := service.gc.StageInScope(ctx, repository.BindGC(tx), ids); err != nil {
		return fmt.Errorf("stage GC candidates: %w", err)
	}
	return nil
}

// StageCandidates keeps reference removal and the GC handoff in the caller's transaction.
func (service *Service) StageCandidates(ctx context.Context, tx *sql.Tx, ids []string) error {
	return service.stageCandidates(ctx, tx, ids)
}

func (service *Service) executeBlobGC(ctx context.Context, job claimedJob) error {
	if job.ScopeType != ScopeBlob || len(job.Input.Inputs.SHA256) != 64 {
		return releaseFailure("BLOB_GC_INPUT_INVALID")
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payloadrelease/GC transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := service.fenceWork(ctx, transaction, job); err != nil {
		return err
	}
	protected, err := blobregistry.ProtectiveSet(ctx, transaction)
	if err != nil {
		return fmt.Errorf("payloadrelease/GC protection: %w", err)
	}
	if _, keep := protected[job.ScopeID]; keep {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM blob_gc_candidates WHERE blob_id=?`, job.ScopeID); err != nil {
			return fmt.Errorf("payloadrelease/GC cancel candidate: %w", err)
		}
		if err := service.commitWork(ctx, transaction, job); err != nil {
			return fmt.Errorf("payloadrelease/GC cancel commit: %w", err)
		}
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx, `DELETE FROM archive_entries WHERE archive_blob_id=?`, job.ScopeID,
	); err != nil {
		return fmt.Errorf("payloadrelease/GC archive: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM blob_gc_candidates WHERE blob_id=?`, job.ScopeID); err != nil {
		return fmt.Errorf("payloadrelease/GC candidate: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM blobs WHERE id=?`, job.ScopeID); err != nil {
		return fmt.Errorf("payloadrelease/GC blob: %w", err)
	}
	if err := service.commitWork(ctx, transaction, job); err != nil {
		return fmt.Errorf("payloadrelease/GC commit: %w", err)
	}
	path := service.blobs.Path(job.Input.Inputs.SHA256)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return releaseFailure("BLOB_GC_PHYSICAL_DELETE_FAILED")
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
