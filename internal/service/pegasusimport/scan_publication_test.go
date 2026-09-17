package pegasusimport

import (
	"context"
	"errors"
	model "retrom/internal/model/pegasusimport"
	"testing"
	"time"
)

type publicationFake struct {
	before     model.ExecutionSnapshot
	err        error
	shape      model.ScanShape
	writes     []string
	batchSizes []int
}

func (fake *publicationFake) WithScan(_ context.Context, work func(model.ScanScope) error) error {
	return work(model.ScanScope{Read: fake, Write: fake})
}

func (fake *publicationFake) Current(context.Context, string) (model.ExecutionSnapshot, error) {
	return fake.before, fake.err
}

func (fake *publicationFake) Shape(context.Context, string) (model.ScanShape, error) {
	return fake.shape, fake.err
}

func (fake *publicationFake) Headers(_ context.Context, _ model.ScanLease, headers model.ScanHeaders) error {
	fake.writes = append(fake.writes, "headers")
	fake.batchSizes = append(fake.batchSizes, len(headers.Metadata)+len(headers.Collections))
	return nil
}

func TestScanPublicationBoundsCollectionAndMetadataTransactions(t *testing.T) {
	t.Parallel()
	service, fake, id := publicationServiceFixture()
	headers := model.ScanHeaders{Metadata: make([]model.ScanMetadata, 1000), Collections: make([]model.ScanCollection, 1001)}
	if err := service.Headers(t.Context(), id, headers); err != nil {
		t.Fatal(err)
	}
	if len(fake.batchSizes) != 5 {
		t.Fatalf("header transactions=%v", fake.batchSizes)
	}
	for _, size := range fake.batchSizes {
		if size > 500 {
			t.Fatalf("unbounded header transaction=%d", size)
		}
	}
}

func (fake *publicationFake) Items(context.Context, model.ScanLease, []model.ScanItem) error {
	fake.writes = append(fake.writes, "items")
	return nil
}

func (fake *publicationFake) Finish(context.Context, model.ScanLease, model.ScanSummary) error {
	fake.writes = append(fake.writes, "finish")
	return nil
}

func publicationServiceFixture() (*ScanPublication, *publicationFake, model.ExecutionIdentity) {
	fake := &publicationFake{
		before: model.ExecutionSnapshot{
			JobID: "scan", ImportID: "plan", WorkerID: "owner", Kind: "SERVER_PEGASUS_SCAN",
			JobState: "RUNNING", ImportState: "SCANNING", JobVersion: 1, ImportVersion: 1, ExecutionNo: 1, Attempt: 1, LeaseUntilMS: 90, DeadlineMS: 100,
		},
	}
	return NewScanPublication(fake, func() time.Time { return time.UnixMilli(10) }), fake, model.ExecutionIdentity{
		JobID: "scan", ImportID: "plan", WorkerID: "owner", ExecutionNo: 1, Attempt: 1,
	}
}

func TestScanPublicationValidatesOwnerAndPreservesReadCause(t *testing.T) {
	t.Parallel()
	service, fake, id := publicationServiceFixture()
	id.WorkerID = "old"
	if err := service.Headers(
		t.Context(),
		id,
		model.ScanHeaders{},
	); !errors.Is(
		err,
		model.ErrVersionConflict,
	) || len(
		fake.writes,
	) != 0 {
		t.Fatalf("stale owner wrote: %v %v", fake.writes, err)
	}
	cause := errors.New("scan read unavailable")
	fake.err = cause
	if err := service.Headers(t.Context(), id, model.ScanHeaders{}); !errors.Is(err, cause) {
		t.Fatalf("lost read cause: %v", err)
	}
}

func TestScanPublicationCannotPublishMissingBatches(t *testing.T) {
	t.Parallel()
	service, fake, id := publicationServiceFixture()
	if err := service.Finish(
		t.Context(),
		id,
		model.ScanSummary{Shape: model.ScanShape{Items: 1}},
	); !errors.Is(
		err,
		model.ErrVersionConflict,
	) || len(
		fake.writes,
	) != 0 {
		t.Fatalf("published missing items: %v %v", fake.writes, err)
	}
}

func TestScanPublicationRejectsOversizedBatchBeforeTransaction(t *testing.T) {
	t.Parallel()
	service, fake, id := publicationServiceFixture()
	if err := service.Items(
		t.Context(),
		id,
		make([]model.ScanItem, 501),
	); !errors.Is(
		err,
		model.ErrScanLimit,
	) || len(
		fake.writes,
	) != 0 {
		t.Fatalf("unbounded scan transaction: %v %v", fake.writes, err)
	}
}
