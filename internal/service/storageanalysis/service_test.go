package storageanalysis

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/storageanalysis"
)

type snapshotRepository struct {
	value model.ReadModel
	err   error
	reads int
}

func (repository *snapshotRepository) Read(context.Context) (model.ReadModel, error) {
	repository.reads++
	return repository.value, repository.err
}

func TestAnalyzePropagatesArchiveUsageWithinOneSnapshot(t *testing.T) {
	t.Parallel()
	repository := &snapshotRepository{value: model.ReadModel{
		Blobs:     map[string]int64{"archive": 100, "member": 80, "orphan": 20},
		Protected: map[string]struct{}{"archive": {}, "member": {}},
		Usage:     map[string]model.Usage{"archive": model.UsageGame},
		Archives:  []model.ArchiveMember{{ArchiveID: "archive", MemberID: "member"}},
		Saves:     model.SaveReferences{ActiveCount: 1, PayloadIDs: []string{"member"}}, CleanupCandidates: []string{"orphan"},
	}}
	snapshot, err := New(repository, func() time.Time { return time.UnixMilli(1234) }).Analyze(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if repository.reads != 1 {
		t.Fatalf("read %d snapshots", repository.reads)
	}
	if snapshot.GeneratedAtMS != 1234 || snapshot.Totals.ProtectedBytes != 180 || snapshot.Totals.UnreferencedBytes != 20 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Categories[0].Bytes != 180 || snapshot.Categories[0].BlobCount != 2 {
		t.Fatalf("archive usage = %#v", snapshot.Categories[0])
	}
	if snapshot.Details.SaveStates.StateReferenceBytes != 80 || snapshot.Details.CleanupCandidates.Bytes != 20 {
		t.Fatalf("details = %#v", snapshot.Details)
	}
}

func TestAnalyzeRejectsMissingProtectedBlob(t *testing.T) {
	t.Parallel()
	repository := &snapshotRepository{value: model.ReadModel{Blobs: map[string]int64{}, Protected: map[string]struct{}{"missing": {}}}}
	if _, err := New(repository, time.Now).Analyze(t.Context()); !errors.Is(err, errProtectedBlobMissing) {
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
