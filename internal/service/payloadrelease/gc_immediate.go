package payloadrelease

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	model "retrom/internal/model/payloadrelease"
)

func (service *GCScheduler) Immediate(ctx context.Context, actor string) (model.ImmediateGCResult, error) {
	if actor == "" {
		return model.ImmediateGCResult{}, model.ErrImmediateGCAuditActorMissing
	}
	if err := service.Reconcile(ctx); err != nil {
		return model.ImmediateGCResult{}, err
	}
	var result model.ImmediateGCResult
	err := service.repository.WithGC(ctx, func(scope model.GCScope) error {
		facts, err := scope.Read.Candidates(ctx)
		if err != nil {
			return fmt.Errorf("read immediate GC candidates: %w", err)
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
			return fmt.Errorf("fence immediate GC candidates: %w", err)
		}
		for _, change := range changes {
			if err := scope.Write.Advance(ctx, change); err != nil {
				return fmt.Errorf("advance immediate GC candidate: %w", err)
			}
		}
		if err := scope.Write.Audit(ctx, audit); err != nil {
			return fmt.Errorf("audit immediate GC: %w", err)
		}
		result = prepared
		return nil
	})
	if err != nil {
		return model.ImmediateGCResult{}, fmt.Errorf("commit immediate GC: %w", err)
	}
	if service.wake != nil {
		service.wake()
	}
	return result, nil
}

func (service *GCScheduler) immediateChanges(facts []model.GCBlob) ([]model.GCBlob, []model.GCAdvance, model.ImmediateGCResult, error) {
	result := model.ImmediateGCResult{AcceptedAtMS: service.now().UnixMilli()}
	var selected []model.GCBlob
	var changes []model.GCAdvance
	if result.AcceptedAtMS < 0 {
		return nil, nil, model.ImmediateGCResult{}, model.ErrInputInvalid
	}
	for _, blob := range facts {
		if blob.Protected {
			continue
		}
		if err := validGCCandidate(blob); err != nil {
			return nil, nil, model.ImmediateGCResult{}, err
		}
		if blob.SizeBytes < 0 || blob.SizeBytes > math.MaxInt64-result.Bytes {
			return nil, nil, model.ImmediateGCResult{}, model.ErrImmediateGCBytesOverflow
		}
		change := model.GCAdvance{
			Before: blob, NowMS: result.AcceptedAtMS,
			ScheduledMS: max(blob.Candidate.FirstUnreferencedMS, result.AcceptedAtMS),
		}
		if blob.Candidate.Work.State == "FAILED" {
			retry, err := service.retry(blob)
			if err != nil {
				return nil, nil, model.ImmediateGCResult{}, err
			}
			change.Retry = &retry
		}
		selected = append(selected, blob)
		changes = append(changes, change)
		result.BlobCount++
		result.Bytes += blob.SizeBytes
	}
	return selected, changes, result, nil
}

func validGCCandidate(blob model.GCBlob) error {
	work := blob.Candidate.Work
	if !blob.HasCandidate || work.ID == "" || work.Scope != (model.Scope{Type: model.ScopeBlob, ID: blob.ID}) ||
		work.Kind != "BLOB_GC" || work.Version < 1 || work.Version == math.MaxInt64 ||
		work.ExecutionNo < 1 || work.ExecutionNo == math.MaxInt64 ||
		(work.State != "QUEUED" && work.State != "RUNNING" && work.State != "FAILED") {
		return model.ErrImmediateGCJobStateInvalid
	}
	return nil
}

func (service *GCScheduler) retry(blob model.GCBlob) (model.GCRetry, error) {
	encoded, digest, err := service.input(blob)
	if err != nil {
		return model.GCRetry{}, err
	}
	next := blob.Candidate.Work.ExecutionNo + 1
	return model.GCRetry{
		ExecutionNo: next, InputJSON: encoded, InputDigest: digest,
		PayloadJSON: fmt.Sprintf(`{"inputExecutionNo":%d}`, next),
		EventJSON:   fmt.Sprintf(`{"schemaVersion":1,"executionNo":%d,"reason":"IMMEDIATE_STORAGE_CLEANUP"}`, next),
	}, nil
}

func (service *GCScheduler) audit(actor string, result model.ImmediateGCResult) (model.GCAudit, error) {
	id, err := service.newID()
	if err != nil || id == "" {
		return model.GCAudit{}, fmt.Errorf("GC audit identity: %w", gcIdentityError(err))
	}
	encoded, err := json.Marshal(struct {
		SchemaVersion int   `json:"schemaVersion"`
		Count         int64 `json:"scheduledBlobCount"`
		Bytes         int64 `json:"scheduledBytes"`
	}{1, result.BlobCount, result.Bytes})
	if err != nil {
		return model.GCAudit{}, fmt.Errorf("encode GC audit: %w", err)
	}
	return model.GCAudit{ID: id, ActorUserID: actor, AfterJSON: string(encoded), NowMS: result.AcceptedAtMS}, nil
}

func (service *GCScheduler) cancelProtected(ctx context.Context) error {
	err := service.repository.WithGC(ctx, func(scope model.GCScope) error {
		facts, err := scope.Read.Candidates(ctx)
		if err != nil {
			return fmt.Errorf("read protected GC candidates: %w", err)
		}
		var selected []model.GCBlob
		for _, blob := range facts {
			if blob.Protected {
				if err := validGCCandidate(blob); err != nil {
					return err
				}
				selected = append(selected, blob)
			}
		}
		if err := scope.Write.Fence(ctx, selected); err != nil {
			return fmt.Errorf("fence protected GC candidates: %w", err)
		}
		now := service.now().UnixMilli()
		for _, blob := range selected {
			if err := scope.Write.Cancel(ctx, model.GCCancellation{
				Before: blob, NowMS: now,
				Complete:  blob.Candidate.Work.State == "QUEUED",
				EventJSON: `{"schemaVersion":1,"reason":"REFERENCE_RESTORED"}`,
			}); err != nil {
				return fmt.Errorf("cancel protected GC candidate: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("cancel protected GC candidates: %w", err)
	}
	return nil
}
