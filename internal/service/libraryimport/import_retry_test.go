package libraryimport

import (
	"context"
	"errors"
	model "retrom/internal/model/libraryimport"
	"strings"
	"testing"
	"time"
)

type importRetryFixture struct {
	snapshot             model.ImportItemRetrySnapshot
	found                bool
	currentErr, writeErr error
	transactions         int
	write                model.ImportItemRetryWrite
}

func (fixture *importRetryFixture) WithRetry(
	_ context.Context, work func(model.ImportItemRetryScope) error,
) error {
	fixture.transactions++
	return work(fixture)
}

func (fixture *importRetryFixture) Current(
	context.Context, string,
) (model.ImportItemRetrySnapshot, bool, error) {
	return fixture.snapshot, fixture.found, fixture.currentErr
}

func (fixture *importRetryFixture) Retry(
	_ context.Context, write model.ImportItemRetryWrite,
) error {
	fixture.write = write
	return fixture.writeErr
}

func newImportRetryFixture() *importRetryFixture {
	return &importRetryFixture{
		found: true,
		snapshot: model.ImportItemRetrySnapshot{
			ImportID: "import", Stage: "METADATA", ManifestDigest: strings.Repeat("a", 64), Version: 3,
			State: "FAILED_RETRYABLE",
		},
	}
}

func importRetryService(fixture *importRetryFixture) *ImportItemRetries {
	service := NewImportItemRetries(fixture, func() time.Time { return time.UnixMilli(1000) })
	service.newID = func() (string, error) { return "job", nil }
	return service
}

func TestImportItemRetryRejectsInvalidInputBeforeTransaction(t *testing.T) {
	for _, request := range []model.ImportItemRetryRequest{{}, {ItemID: "item"}, {ItemID: "item", ExpectedVersion: 0}} {
		fixture := newImportRetryFixture()
		_, err := importRetryService(fixture).Retry(t.Context(), request)
		if !errors.Is(err, model.ErrInvalid) || fixture.transactions != 0 {
			t.Fatalf("request=%+v result error=%v transactions=%d", request, err, fixture.transactions)
		}
	}
}

func TestImportItemRetryBuildsDurableWrite(t *testing.T) {
	fixture := newImportRetryFixture()
	result, err := importRetryService(fixture).Retry(t.Context(), model.ImportItemRetryRequest{ItemID: "item", ExpectedVersion: 3})
	if err != nil {
		t.Fatalf("retry error=%v", err)
	}
	if result != (model.ImportItemRetryResult{ItemID: "item", JobID: "job", State: "QUEUED", Version: 4}) {
		t.Fatalf("retry result=%+v", result)
	}
	if fixture.write.ItemID != "item" || fixture.write.ImportID != "import" || fixture.write.Stage != "METADATA" ||
		fixture.write.ExpectedVersion != 3 || fixture.write.JobID != "job" || fixture.write.NowMS != 1000 ||
		fixture.write.DedupeKey == "" || fixture.write.PayloadJSON != `{"sourceManifestDigest":"`+strings.Repeat("a", 64)+`"}` {
		t.Fatalf("retry write=%+v", fixture.write)
	}
}

func TestImportItemRetryPreservesStorageAndStateErrors(t *testing.T) {
	cause := errors.New("retry storage unavailable")
	for _, test := range []struct {
		name   string
		mutate func(*importRetryFixture)
		expect error
	}{
		{"read", func(f *importRetryFixture) { f.currentErr = cause }, cause},
		{"write", func(f *importRetryFixture) { f.writeErr = cause }, cause},
		{"missing", func(f *importRetryFixture) { f.found = false }, model.ErrInvalid},
		{"stale", func(f *importRetryFixture) { f.snapshot.Version = 4 }, model.ErrInvalid},
		{"terminal", func(f *importRetryFixture) { f.snapshot.State = "COMPLETED" }, model.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newImportRetryFixture()
			test.mutate(fixture)
			result, err := importRetryService(fixture).Retry(t.Context(), model.ImportItemRetryRequest{ItemID: "item", ExpectedVersion: 3})
			if !errors.Is(err, test.expect) || result != (model.ImportItemRetryResult{}) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
