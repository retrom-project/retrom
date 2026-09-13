package libraryimport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type importBatchCancellationFixture struct {
	result  ImportBatchCancellationResult
	err     error
	request ImportBatchCancellationRequest
	now     int64
	calls   int
}

func (fixture *importBatchCancellationFixture) Cancel(
	_ context.Context, request ImportBatchCancellationRequest, now int64,
) (ImportBatchCancellationResult, error) {
	fixture.calls++
	fixture.request, fixture.now = request, now
	return fixture.result, fixture.err
}

func TestImportBatchCancellationValidatesBeforeRepository(t *testing.T) {
	requests := []ImportBatchCancellationRequest{
		{},
		{ImportID: "import"},
		{ImportID: "import", ExpectedVersion: 1},
		{ImportID: "import", ExpectedVersion: 1, Reason: strings.Repeat("中", 501)},
	}
	for _, request := range requests {
		fixture := &importBatchCancellationFixture{}
		_, err := NewImportBatchCancellations(fixture, func() time.Time { return time.UnixMilli(5) }).Cancel(t.Context(), request)
		if !errors.Is(err, ErrInvalid) || fixture.calls != 0 {
			t.Fatalf("request=%+v error=%v calls=%d", request, err, fixture.calls)
		}
	}
}

func TestImportBatchCancellationNormalizesAndPassesAtomicRequest(t *testing.T) {
	fixture := &importBatchCancellationFixture{result: ImportBatchCancellationResult{
		ImportID: "import", GroupJobID: "group-job", State: "CANCEL_REQUESTED", Version: 4, Pending: true,
	}}
	result, err := NewImportBatchCancellations(fixture, func() time.Time { return time.UnixMilli(88) }).Cancel(
		t.Context(), ImportBatchCancellationRequest{ImportID: "import", ExpectedVersion: 3, Reason: "  stop  ", PreserveReviews: true},
	)
	if err != nil || result != fixture.result {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if fixture.request.Reason != "stop" || !fixture.request.PreserveReviews || fixture.now != 88 || fixture.calls != 1 {
		t.Fatalf("repository request=%+v now=%d calls=%d", fixture.request, fixture.now, fixture.calls)
	}
}

func TestImportBatchCancellationPreservesRepositoryError(t *testing.T) {
	cause := errors.New("cancel database unavailable")
	fixture := &importBatchCancellationFixture{err: cause}
	_, err := NewImportBatchCancellations(fixture, func() time.Time { return time.UnixMilli(1) }).Cancel(
		t.Context(), ImportBatchCancellationRequest{ImportID: "import", ExpectedVersion: 1, Reason: "stop"},
	)
	if !errors.Is(err, cause) {
		t.Fatalf("error=%v", err)
	}
}
