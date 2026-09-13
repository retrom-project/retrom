package launch

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestPreviewCreatorPreservesBothReplayBoundaries(t *testing.T) {
	t.Parallel()
	for _, final := range []bool{false, true} {
		t.Run(map[bool]string{false: "before source", true: "before insert"}[final], func(t *testing.T) {
			creator, repository, provider, request := previewFixture(t)
			receipt := &PreviewReceipt{ID: previewTestID, ImportItemID: request.ImportItemID}
			if final {
				repository.finalReceipt = receipt
				repository.missingCurrent = true
			} else {
				repository.receipt = receipt
				provider.absent = true
				repository.missingSnapshot = true
			}
			result, err := creator.Create(t.Context(), request)
			if err != nil || result.PreviewID != previewTestID || len(repository.writes) != 0 {
				t.Fatalf("replay: id=%q writes=%d error=%v", result.PreviewID, len(repository.writes), err)
			}
			if !final && (repository.loads != 0 || repository.transactions != 0) {
				t.Fatal("replay rebuilt an expired or closed receipt")
			}
			receipt.ImportItemID = "different"
			if result, err := creator.Create(t.Context(), request); !errors.Is(err, ErrReviewPreviewUnavailable) || result.PreviewID != "" {
				t.Fatalf("cross-item replay: %v", err)
			}
			receipt.ImportItemID = request.ImportItemID
			previous := "previous"
			receipt.RestoreFromPreviewID = &previous
			if _, err := creator.Create(t.Context(), request); !errors.Is(err, ErrReviewPreviewUnavailable) {
				t.Fatalf("restore mismatch replay: %v", err)
			}
		})
	}
}

func TestPreviewCreatorRejectsFinalSourceDrift(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*previewTestRepository)
	}{
		{"missing owner", func(r *previewTestRepository) { r.missingCurrent = true }},
		{"source", func(r *previewTestRepository) { r.current.SourceSnapshotID = "changed" }},
		{"platform", func(r *previewTestRepository) { r.current.PlatformInstanceID = "changed" }},
		{"provider", func(r *previewTestRepository) { r.current.ProviderID = "changed" }},
		{"target", func(r *previewTestRepository) { r.current.TargetID = "changed" }},
		{"bundle", func(r *previewTestRepository) { r.current.BundleSHA256 = "changed" }},
		{"core", func(r *previewTestRepository) { r.current.CoreID = "changed" }},
		{"validation", func(r *previewTestRepository) { r.current.ValidationID = "changed" }},
		{"dependencies", func(r *previewTestRepository) { r.current.DependencySnapshot = "changed" }},
		{"DOS", func(r *previewTestRepository) { value := "new.exe"; r.current.DefaultDOSEntry = &value }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			creator, repository, _, request := previewFixture(t)
			test.change(repository)
			result, err := creator.Create(t.Context(), request)
			if !errors.Is(err, ErrReviewPreviewUnavailable) || result.PreviewID != "" || len(repository.writes) != 0 {
				t.Fatalf("stale source wrote: id=%q error=%v", result.PreviewID, err)
			}
		})
	}
}

func TestPreviewCreatorChecksClockBounds(t *testing.T) {
	t.Parallel()
	for _, now := range []int64{-1, math.MaxInt64 - 7200000 + 1} {
		creator, repository, _, request := previewFixture(t)
		creator.environment.Now = func() time.Time { return time.UnixMilli(now) }
		if result, err := creator.Create(t.Context(), request); !errors.Is(err, ErrReviewPreviewUnavailable) || result.PreviewID != "" || len(repository.writes) != 0 {
			t.Fatalf("invalid clock %d: %v", now, err)
		}
	}
}

func TestPreviewCreatorSignsIsolationOutsideTransaction(t *testing.T) {
	t.Parallel()
	creator, repository, _, request := previewFixture(t)
	repository.snapshot.Source.DeliveryProfile = "ISOLATED_WEB_PROJECT"
	repository.current = repository.snapshot.Source
	calls := 0
	creator.environment.SignIsolation = func(id string) (IsolationTicket, error) {
		if repository.inTransaction || id != previewTestID {
			t.Fatal("isolated ticket signed inside transaction or for another session")
		}
		calls++
		return IsolationTicket{Origin: "https://preview.example", Hash: [32]byte{1}}, nil
	}
	result, err := creator.Create(t.Context(), request)
	if err != nil || result.PreviewID == "" || calls != 1 || len(repository.writes) != 1 {
		t.Fatalf("isolation: calls=%d error=%v", calls, err)
	}
	plan := repository.writes[0]
	if plan.Isolation == nil || plan.ProfileID != "profile" || plan.Isolation.Origin != "https://preview.example" {
		t.Fatal("isolation owner or origin changed")
	}
}
