package storageanalysis

import (
	"context"
	"errors"
	"testing"
	"time"
)

type snapshotRepository struct {
	value ReadModel
	err   error
	reads int
}

func (repository *snapshotRepository) Read(context.Context) (ReadModel, error) {
	repository.reads++
	return repository.value, repository.err
}

func TestAnalyzeCountsIndependentFilesWithinOneSnapshot(t *testing.T) {
	t.Parallel()
	repository := &snapshotRepository{value: ReadModel{
		Blobs:    map[string]int64{"archive": 100, "member": 80, "orphan": 20},
		Retained: map[string]struct{}{"archive": {}, "member": {}},
		Usage:    map[string]Usage{"archive": UsageGame, "member": UsageGame},
		Saves:    SaveReferences{ActiveCount: 1, PayloadIDs: []string{"member"}}, CleanupCandidates: []string{"orphan"},
	}}
	snapshot, err := New(repository, func() time.Time { return time.UnixMilli(1234) }).Analyze(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if repository.reads != 1 {
		t.Fatalf("read %d snapshots", repository.reads)
	}
	if snapshot.GeneratedAtMS != 1234 || snapshot.Totals.RetainedBytes != 180 || snapshot.Totals.PendingDeleteBytes != 20 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Categories[0].Bytes != 180 || snapshot.Categories[0].FileCount != 2 {
		t.Fatalf("archive usage = %#v", snapshot.Categories[0])
	}
	if snapshot.Details.SaveStates.StateBytes != 80 || snapshot.Details.CleanupCandidates.Bytes != 20 {
		t.Fatalf("details = %#v", snapshot.Details)
	}
}

func TestAnalyzeRejectsMissingProtectedBlob(t *testing.T) {
	t.Parallel()
	repository := &snapshotRepository{value: ReadModel{Blobs: map[string]int64{}, Retained: map[string]struct{}{"missing": {}}}}
	if _, err := New(repository, time.Now).Analyze(t.Context()); !errors.Is(err, errRetainedFileMissing) {
		t.Fatalf("missing protected blob: %v", err)
	}
}

func TestAnalyzePreservesReadFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("snapshot unavailable")
	repository := &snapshotRepository{err: cause}
	if _, err := New(repository, time.Now).Analyze(t.Context()); !errors.Is(err, cause) {
		t.Fatalf("read error: %v", err)
	}
}
