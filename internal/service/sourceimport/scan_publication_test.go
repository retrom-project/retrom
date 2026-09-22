package sourceimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type publicationFake struct {
	before     ExecutionSnapshot
	err        error
	shape      ScanShape
	writes     []string
	batchSizes []int
}

func (fake *publicationFake) WithScan(_ context.Context, work func(ScanScope) error) error {
	return work(ScanScope{Read: fake, Write: fake})
}

func (fake *publicationFake) Current(context.Context, string) (ExecutionSnapshot, error) {
	return fake.before, fake.err
}

func (fake *publicationFake) Shape(context.Context, string) (ScanShape, error) {
	return fake.shape, fake.err
}

func (fake *publicationFake) Headers(_ context.Context, _ ScanLease, headers ScanHeaders) error {
	fake.writes = append(fake.writes, "headers")
	fake.batchSizes = append(fake.batchSizes, len(headers.Metadata)+len(headers.Collections))
	return nil
}

func TestScanPublicationBoundsCollectionAndMetadataTransactions(t *testing.T) {
	t.Parallel()
	service, fake, id := publicationServiceFixture()
	headers := ScanHeaders{Metadata: make([]ScanMetadata, 1000), Collections: make([]ScanCollection, 1001)}
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

func (fake *publicationFake) Items(context.Context, ScanLease, []ScanItem) error {
	fake.writes = append(fake.writes, "items")
	return nil
}

func (fake *publicationFake) Finish(context.Context, ScanLease, ScanSummary) error {
	fake.writes = append(fake.writes, "finish")
	return nil
}

func publicationServiceFixture() (*ScanPublication, *publicationFake, ExecutionIdentity) {
	fake := &publicationFake{
		before: ExecutionSnapshot{
			JobID: "scan", ImportID: "plan", WorkerID: "owner", Kind: "IMPORT_SCAN",
			JobState: "RUNNING", ImportState: "SCANNING", JobVersion: 1, ImportVersion: 1, ExecutionNo: 1, Attempt: 1, LeaseUntilMS: 90, DeadlineMS: 100,
		},
	}
	return NewScanPublication(fake, func() time.Time { return time.UnixMilli(10) }), fake, ExecutionIdentity{
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
		ScanHeaders{},
	); !errors.Is(
		err,
		ErrVersionConflict,
	) || len(
		fake.writes,
	) != 0 {
		t.Fatalf("stale owner wrote: %v %v", fake.writes, err)
	}
	cause := errors.New("scan read unavailable")
	fake.err = cause
	if err := service.Headers(t.Context(), id, ScanHeaders{}); !errors.Is(err, cause) {
		t.Fatalf("lost read cause: %v", err)
	}
}

func TestScanPublicationCannotPublishMissingBatches(t *testing.T) {
	t.Parallel()
	service, fake, id := publicationServiceFixture()
	if err := service.Finish(
		t.Context(),
		id,
		ScanSummary{Shape: ScanShape{Items: 1}},
	); !errors.Is(
		err,
		ErrVersionConflict,
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
		make([]ScanItem, 501),
	); !errors.Is(
		err,
		ErrScanLimit,
	) || len(
		fake.writes,
	) != 0 {
		t.Fatalf("unbounded scan transaction: %v %v", fake.writes, err)
	}
}
