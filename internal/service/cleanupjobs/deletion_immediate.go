package cleanupjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

func (service *DeletionScheduler) Immediate(ctx context.Context, actor string) (ImmediateDeletionResult, error) {
	if actor == "" {
		return ImmediateDeletionResult{}, ErrImmediateDeletionAuditActorMissing
	}
	if err := service.Reconcile(ctx); err != nil {
		return ImmediateDeletionResult{}, err
	}
	var result ImmediateDeletionResult
	err := service.repository.WithDeletion(ctx, func(scope DeletionScope) error {
		facts, err := scope.Read.Candidates(ctx)
		if err != nil {
			return fmt.Errorf("read immediate file deletion candidates: %w", err)
		}
		selected, changes, prepared, err := service.immediateChanges(facts)
		if err != nil {
			return err
		}
		audit, err := service.audit(actor, prepared)
		if err != nil {
			return err
		}
		if err := scope.Write.Fence(ctx, selected); err != nil {
			return fmt.Errorf("fence immediate file deletion candidates: %w", err)
		}
		for _, change := range changes {
			if err := scope.Write.Advance(ctx, change); err != nil {
				return fmt.Errorf("advance immediate file deletion candidate: %w", err)
			}
		}
		if err := scope.Write.Audit(ctx, audit); err != nil {
			return fmt.Errorf("audit immediate file deletion: %w", err)
		}
		result = prepared
		return nil
	})
	if err != nil {
		return ImmediateDeletionResult{}, fmt.Errorf("commit immediate file deletion: %w", err)
	}
	if service.wake != nil {
		service.wake()
	}
	return result, nil
}

func (service *DeletionScheduler) immediateChanges(
	facts []DeletionFile,
) ([]DeletionFile, []DeletionAdvance, ImmediateDeletionResult, error) {
	result := ImmediateDeletionResult{AcceptedAtMS: service.now().UnixMilli()}
	var selected []DeletionFile
	var changes []DeletionAdvance
	if result.AcceptedAtMS < 0 {
		return nil, nil, ImmediateDeletionResult{}, ErrInputInvalid
	}
	for _, blob := range facts {
		if blob.Retained {
			continue
		}
		if err := validDeletionCandidate(blob); err != nil {
			return nil, nil, ImmediateDeletionResult{}, err
		}
		if blob.SizeBytes < 0 || blob.SizeBytes > math.MaxInt64-result.Bytes {
			return nil, nil, ImmediateDeletionResult{}, ErrImmediateDeletionBytesOverflow
		}
		change := DeletionAdvance{
			Before: blob, NowMS: result.AcceptedAtMS,
			ScheduledMS: max(blob.Candidate.RetiredMS, result.AcceptedAtMS),
		}
		if blob.Candidate.Work.State == "FAILED" {
			retry, err := service.retry(blob)
			if err != nil {
				return nil, nil, ImmediateDeletionResult{}, err
			}
			change.Retry = &retry
		}
		selected = append(selected, blob)
		changes = append(changes, change)
		result.FileCount++
		result.Bytes += blob.SizeBytes
	}
	return selected, changes, result, nil
}

func validDeletionCandidate(blob DeletionFile) error {
	work := blob.Candidate.Work
	if !blob.HasCandidate || work.ID == "" || work.Scope != (Scope{Type: ScopeFile, ID: blob.ID}) ||
		work.Kind != "FILE_DELETE" || work.Version < 1 || work.Version == math.MaxInt64 ||
		work.ExecutionNo < 1 || work.ExecutionNo == math.MaxInt64 ||
		(work.State != "QUEUED" && work.State != "RUNNING" && work.State != "FAILED") {
		return ErrImmediateDeletionJobStateInvalid
	}
	return nil
}

func (service *DeletionScheduler) retry(blob DeletionFile) (DeletionRetry, error) {
	encoded, digest, err := service.input(blob)
	if err != nil {
		return DeletionRetry{}, err
	}
	next := blob.Candidate.Work.ExecutionNo + 1
	return DeletionRetry{
		ExecutionNo: next, InputJSON: encoded, InputDigest: digest,
		PayloadJSON: fmt.Sprintf(`{"inputExecutionNo":%d}`, next),
		EventJSON:   fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"reason":"IMMEDIATE_STORAGE_CLEANUP"}`, next),
	}, nil
}

func (service *DeletionScheduler) audit(actor string, result ImmediateDeletionResult) (DeletionAudit, error) {
	id, err := service.newID()
	if err != nil || id == "" {
		return DeletionAudit{}, fmt.Errorf("file deletion audit identity: %w", deletionIdentityError(err))
	}
	encoded, err := json.Marshal(struct {
		SchemaVersion int   `json:"schemaVersion"`
		Count         int64 `json:"scheduledFileCount"`
		Bytes         int64 `json:"scheduledBytes"`
	}{1, result.FileCount, result.Bytes})
	if err != nil {
		return DeletionAudit{}, fmt.Errorf("encode file deletion audit: %w", err)
	}
	return DeletionAudit{ID: id, ActorUserID: actor, AfterJSON: string(encoded), NowMS: result.AcceptedAtMS}, nil
}
