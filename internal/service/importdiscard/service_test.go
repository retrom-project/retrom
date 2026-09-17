package importdiscard

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/importdiscard"
)

const batchID = "01980000-0000-7000-8000-000000009981"

type memoryRecords struct {
	batch       model.Batch
	disposition *model.Disposition
	requests    []model.Request
	fail        error
}

func (records *memoryRecords) Batch(context.Context, model.Key) (model.Batch, error) {
	return records.batch, records.fail
}

func (records *memoryRecords) Disposition(context.Context, model.Key) (model.Disposition, bool, error) {
	if records.disposition == nil {
		return model.Disposition{}, false, nil
	}
	return *records.disposition, true, nil
}

func (records *memoryRecords) Request(_ context.Context, request model.Request) error {
	records.requests = append(records.requests, request)
	return records.fail
}

func (records *memoryRecords) Progress(context.Context, model.Progress) error { return records.fail }

type memoryRepository struct {
	records               *memoryRecords
	beforeWrite           func()
	readCount, writeCount int
}

func (repository *memoryRepository) Batch(ctx context.Context, key model.Key) (model.Batch, error) {
	repository.readCount++
	return repository.records.Batch(ctx, key)
}

func (repository *memoryRepository) Disposition(ctx context.Context, key model.Key) (model.Disposition, bool, error) {
	return repository.records.Disposition(ctx, key)
}

func (repository *memoryRepository) Pending(context.Context) (model.Request, bool, error) {
	return model.Request{}, false, nil
}

func (repository *memoryRepository) Children(context.Context, model.Key) ([]string, error) {
	return nil, nil
}

func (repository *memoryRepository) CommitRecoverOwnership(context.Context, model.RecoverOwnershipCommand) error {
	repository.writeCount++
	return nil
}

func (repository *memoryRepository) CommitDiscardSourceItems(context.Context, model.DiscardSourceItemsCommand) (bool, error) {
	repository.writeCount++
	return true, nil
}

func (repository *memoryRepository) CommitRequestDiscard(ctx context.Context, cmd model.RequestDiscardCommand) (model.Status, error) {
	repository.writeCount++
	if repository.beforeWrite != nil {
		repository.beforeWrite()
	}
	batch, err := repository.records.Batch(ctx, cmd.Key)
	if err != nil {
		return model.Status{}, err
	}
	result := model.Status{Kind: cmd.Key.Kind, ImportID: cmd.Key.ID, State: "UNAVAILABLE"}
	if available(cmd.Key.Kind, batch) {
		result.State = "AVAILABLE"
	}
	if repository.records.disposition != nil {
		result.State = repository.records.disposition.State
		result.ErrorCode = repository.records.disposition.ErrorCode
	}
	if result.State == "UNAVAILABLE" {
		return model.Status{}, model.ErrInvalid
	}
	if result.State != "AVAILABLE" && result.State != "FAILED" {
		return result, nil
	}
	if repository.records.fail != nil {
		return model.Status{}, repository.records.fail
	}
	repository.records.requests = append(repository.records.requests, model.Request{Key: cmd.Key, UserID: cmd.UserID, AuditID: "test-audit", Now: cmd.NowMS})
	return model.Status{Kind: cmd.Key.Kind, ImportID: cmd.Key.ID, State: "REQUESTED"}, nil
}

func (repository *memoryRepository) CommitProgress(context.Context, model.Progress) error {
	repository.writeCount++
	return repository.records.fail
}

func TestRequestUsesCurrentWriteSnapshotAndPreservesFailure(t *testing.T) {
	records := &memoryRecords{batch: model.Batch{Started: true, State: "RUNNING"}}
	repository := &memoryRepository{records: records, beforeWrite: func() {
		records.batch = model.Batch{Started: true, State: "COMPLETED", ItemCounts: map[string]int64{"PUBLISHED": 1}}
	}}
	service := New(repository, nil, nil, func() time.Time { return time.UnixMilli(17) })
	if _, err := service.Request(t.Context(), "IMPORT", batchID, "user"); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("stale availability: %v", err)
	}
	if repository.readCount != 0 || repository.writeCount != 1 || len(records.requests) != 0 {
		t.Fatalf("request escaped write snapshot: %+v / %+v", repository, records)
	}
	repository.beforeWrite = nil
	records.fail = context.Canceled
	if _, err := service.Request(t.Context(), "IMPORT", batchID, "user"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost database cause: %v", err)
	}
}

func TestExistingDispositionControlsRequestReplay(t *testing.T) {
	for _, state := range []string{"REQUESTED", "COMPLETED", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			code := "IMPORT_BATCH_DISCARD_RELEASE_FAILED"
			records := &memoryRecords{
				batch: model.Batch{
					Started: true,
					State:   "COMPLETED",
				},
				disposition: &model.Disposition{
					State:     state,
					ErrorCode: &code,
				},
			}
			repository := &memoryRepository{records: records}
			service := New(repository, nil, nil, func() time.Time { return time.UnixMilli(17) })
			result, err := service.Request(t.Context(), "IMPORT", batchID, "user")
			if err != nil {
				t.Fatal(err)
			}
			expected := state
			writes := 0
			if state == "FAILED" {
				expected = "REQUESTED"
				writes = 1
			}
			if result.State != expected || len(records.requests) != writes {
				t.Fatalf("replay %+v / %+v", result, records.requests)
			}
			if writes == 1 && (result.ErrorCode != nil || records.requests[0].Now != 17 || records.requests[0].AuditID == "") {
				t.Fatalf("retry did not create fresh audit evidence: %+v / %+v", result, records.requests)
			}
		})
	}
}

func TestDiscardAvailabilityUsesBusinessFacts(t *testing.T) {
	tests := []struct {
		name, kind string
		batch      model.Batch
		want       bool
	}{
		{"not started", "PEGASUS", model.Batch{State: "QUEUED"}, false},
		{"scan", "PEGASUS", model.Batch{Started: true, State: "SCANNING"}, false},
		{"mapping", "EMULATIONSTATION", model.Batch{Started: true, State: "AWAITING_MAPPING"}, false},
		{"active import", "IMPORT", model.Batch{Started: true, State: "RUNNING"}, true},
		{
			"retained rejections",
			"IMPORT",
			model.Batch{
				Started:          true,
				State:            "COMPLETED",
				Rejected:         2,
				ResolvedRejected: 1,
				PayloadState:     "RETAINED",
			},
			true,
		},
		{
			"released rejections",
			"IMPORT",
			model.Batch{
				Started:          true,
				State:            "COMPLETED",
				Rejected:         2,
				ResolvedRejected: 1,
				PayloadState:     "RELEASED",
			},
			false,
		},
		{
			"source decided",
			"PEGASUS",
			model.Batch{
				Started: true,
				State:   "COMPLETED",
				ItemCounts: map[string]int64{
					"PUBLISHED":        1,
					"REVIEW_DISCARDED": 1,
					"SKIPPED_EXISTING": 1,
				},
			},
			false,
		},
		{
			"source blocked",
			"EMULATIONSTATION",
			model.Batch{
				Started: true,
				State:   "PARTIAL_FAILURE",
				ItemCounts: map[string]int64{
					"BLOCKED_CONTENT": 1,
				},
			},
			true,
		},
		{
			"import review",
			"IMPORT",
			model.Batch{
				Started: true,
				State:   "REVIEW_PENDING",
				ItemCounts: map[string]int64{
					"REVIEW_PENDING": 1,
				},
			},
			true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := available(test.kind, test.batch); got != test.want {
				t.Fatalf("available=%t, want %t", got, test.want)
			}
		})
	}
}

func TestProgressDoesNotMarkFailuresCompleted(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{errors.New("storage failed"), "IMPORT_BATCH_DISCARD_FAILED"},
		{model.ErrReleaseFailed, "IMPORT_BATCH_DISCARD_RELEASE_FAILED"},
		{model.ErrAmbiguousOwner, "IMPORT_BATCH_DISCARD_OWNER_AMBIGUOUS"},
	}
	for _, test := range tests {
		result := progressFor(model.Key{Kind: "IMPORT", ID: batchID}, true, test.err, 17)
		if result.State != "FAILED" || result.CompletedAt != nil || result.ErrorCode == nil || *result.ErrorCode != test.code {
			t.Fatalf("failure progress=%+v", result)
		}
	}
	result := progressFor(model.Key{Kind: "IMPORT", ID: batchID}, true, nil, 17)
	if result.State != "COMPLETED" || result.CompletedAt == nil || *result.CompletedAt != 17 {
		t.Fatalf("completion=%+v", result)
	}
}
