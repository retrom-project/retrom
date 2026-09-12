package launch

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestProductCreatorRejectsFinalInputChanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*ProductSnapshot)
	}{
		{"disabled owner", func(s *ProductSnapshot) { s.Found = false }},
		{"moved game", func(s *ProductSnapshot) { s.Source.InstanceID = "other" }},
		{"core selection", func(s *ProductSnapshot) { s.Source.CoreID = "other" }},
		{"provider upgrade", func(s *ProductSnapshot) { s.Source.BundleSHA256 = "other" }},
		{"source replacement", func(s *ProductSnapshot) { s.Source.SourceManifestDigest = "other" }},
		{"content bytes", func(s *ProductSnapshot) { s.GameFiles[0].BlobID = "other" }},
		{"variant evidence", func(s *ProductSnapshot) { s.Source.DependencySnapshot = "different" }},
		{"target contract", func(s *ProductSnapshot) { s.Source.ReadFormats = []string{"changed"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			creator, repository, _, command := productFixture(t)
			test.change(&repository.current)
			result, err := creator.Create(t.Context(), command)
			if !errors.Is(err, ErrBlocked) || result.Created.LaunchID != "" || len(repository.writes) != 0 || len(repository.receipts) != 0 {
				t.Fatalf("launch=%q error=%v writes=%d receipts=%d", result.Created.LaunchID, err, len(repository.writes), len(repository.receipts))
			}
		})
	}
}

func TestProductCreatorAllowsUnrelatedMetadataVersion(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	repository.current.Source.GameVersion += 7
	result, err := creator.Create(t.Context(), command)
	if err != nil || result.Status != 201 || len(repository.writes) != 1 || len(repository.receipts) != 1 {
		t.Fatalf("status=%d error=%v writes=%d receipts=%d", result.Status, err, len(repository.writes), len(repository.receipts))
	}
	if result.Created.BootstrapExpiresAtMS != 301_000 || result.Created.HardExpiresAtMS != 86_401_000 || bytes.Contains(result.Body, []byte(result.Created.Capability)) {
		t.Fatal("product receipt expired incorrectly or persisted capability")
	}
}

func TestProductCreatorReturnsNoReceiptOnStorageFailure(t *testing.T) {
	cause := errors.New("product storage unavailable")
	for _, test := range []struct {
		name string
		set  func(*productTestRepository)
	}{
		{"replay", func(r *productTestRepository) { r.replayErr = cause }},
		{"snapshot", func(r *productTestRepository) { r.snapshotErr = cause }},
		{"final replay", func(r *productTestRepository) { r.finalReplayErr = cause }},
		{"final authority", func(r *productTestRepository) { r.currentErr = cause }},
		{"owner writes", func(r *productTestRepository) { r.writeErr = cause }},
		{"receipt", func(r *productTestRepository) { r.receiptErr = cause }},
		{"commit", func(r *productTestRepository) { r.commitErr = cause }},
	} {
		t.Run(test.name, func(t *testing.T) {
			creator, repository, _, command := productFixture(t)
			test.set(repository)
			result, err := creator.Create(t.Context(), command)
			if !errors.Is(err, cause) || result.Created.LaunchID != "" || len(result.Body) != 0 {
				t.Fatalf("launch=%q error=%v bodyBytes=%d", result.Created.LaunchID, err, len(result.Body))
			}
		})
	}
}

func TestProductCreatorChecksClockAndEntropy(t *testing.T) {
	for _, now := range []int64{-1, math.MaxInt64 - 86_400_000 + 1} {
		t.Run(time.UnixMilli(now).String(), func(t *testing.T) {
			creator, repository, _, command := productFixture(t)
			creator.environment.Now = func() time.Time { return time.UnixMilli(now) }
			result, err := creator.Create(t.Context(), command)
			if !errors.Is(err, ErrBlocked) || result.Created.LaunchID != "" || len(repository.writes) != 0 {
				t.Fatalf("launch=%q error=%v writes=%d", result.Created.LaunchID, err, len(repository.writes))
			}
		})
	}
	creator, repository, _, command := productFixture(t)
	creator.environment.NewID = func() (string, error) { return "", context.Canceled }
	result, err := creator.Create(t.Context(), command)
	if !errors.Is(err, context.Canceled) || result.Created.LaunchID != "" || repository.transactions != 0 {
		t.Fatalf("launch=%q error=%v transactions=%d", result.Created.LaunchID, err, repository.transactions)
	}
}
