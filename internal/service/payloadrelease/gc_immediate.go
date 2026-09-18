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
	facts, err := service.repository.LoadGCCandidates(ctx)
	if err != nil {
		return model.ImmediateGCResult{},
			fmt.Errorf("commit immediate GC: read candidates: %w", err)
	}
	selected, changes, result, err := service.immediateChanges(facts)
	if err != nil {
		return model.ImmediateGCResult{}, fmt.Errorf("commit immediate GC: %w", err)
	}
	audit, err := service.audit(actor, result)
	if err != nil {
		return model.ImmediateGCResult{}, fmt.Errorf("commit immediate GC: %w", err)
	}
	if err := service.repository.CommitImmediateGC(ctx, model.GCImmediateCommit{
		Selected: selected, Changes: changes, Audit: audit,
	}); err != nil {
		return model.ImmediateGCResult{}, fmt.Errorf("commit immediate GC: %w", err)
	}
	if service.wake != nil {
		service.wake()
	}
	return result, nil
}

func (service *GCScheduler) immediateChanges(
	facts []model.GCBlob,
) ([]model.GCBlob, []model.GCAdvance, model.ImmediateGCResult, error) {
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
	facts, err := service.repository.LoadGCCandidates(ctx)
	if err != nil {
		return fmt.Errorf("cancel protected GC candidates: %w", fmt.Errorf("read protected GC candidates: %w", err))
	}
	var selected []model.GCBlob
	var cancellations []model.GCCancellation
	for _, blob := range facts {
		if blob.Protected {
			if err := validGCCandidate(blob); err != nil {
				return fmt.Errorf("cancel protected GC candidates: %w", err)
			}
			selected = append(selected, blob)
		}
	}
	now := service.now().UnixMilli()
	for _, blob := range selected {
		cancellations = append(cancellations, model.GCCancellation{
			Before: blob, NowMS: now,
			Complete:  blob.Candidate.Work.State == "QUEUED",
			EventJSON: `{"schemaVersion":1,"reason":"REFERENCE_RESTORED"}`,
		})
	}
	if len(selected) > 0 {
		if err := service.repository.CommitGCCancellation(ctx, model.GCCancellationBatch{
			Selected: selected, Cancellations: cancellations,
		}); err != nil {
			return fmt.Errorf("cancel protected GC candidates: %w", err)
		}
	}
	return nil
}
