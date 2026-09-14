package maintenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type memoryRepository struct {
	Repository
	records    *restoreRecords
	snapshot   Snapshot
	inspectErr error
	writes     int
	committed  bool
}

func (repository *memoryRepository) Inspect(context.Context, string) (Snapshot, error) {
	return repository.snapshot, repository.inspectErr
}

func (repository *memoryRepository) WithRestore(_ context.Context, _ string, work func(RestoreRecords) error) error {
	repository.writes++
	err := work(repository.records)
	repository.committed = err == nil
	return err
}

type restoreRecords struct {
	calls     []string
	audit     FenceAudit
	failAudit error
}

type emptyRestoredReviews struct{ RestoredReviewRecords }

func (emptyRestoredReviews) Pending(context.Context, RestoredReviewQuery) ([]RestoredReview, error) {
	return nil, nil
}

func (records *restoreRecords) Imports() RestoredImportScope {
	records.calls = append(records.calls, "reviews")
	return RestoredImportScope{
		Reviews:  RestoredReviewScope{Records: emptyRestoredReviews{}},
		Payloads: RestoredPayloadScope{Records: records},
	}
}

func (records *restoreRecords) RetainedSources(_ context.Context, query RestoredPayloadQuery) ([]string, error) {
	records.calls = append(records.calls, string(query.Kind))
	return nil, nil
}

func (records *restoreRecords) RevokeAccess(_ context.Context, _ int64) (AccessCounts, error) {
	records.calls = append(records.calls, "revoke")
	return AccessCounts{Sessions: 1, Links: 2, Launches: 3}, nil
}

func (records *restoreRecords) StopExternalImports(context.Context, int64) (ImportCounts, error) {
	records.calls = append(records.calls, "external")
	return ImportCounts{BIOS: 4, Pegasus: 5, EmulationStation: 6}, nil
}

func (records *restoreRecords) StopBulkApprovals(context.Context, int64) error {
	records.calls = append(records.calls, "bulk")
	return nil
}

func (records *restoreRecords) Audit(_ context.Context, audit FenceAudit) error {
	records.calls = append(records.calls, "audit")
	records.audit = audit
	return records.failAudit
}

func TestRestoreFenceHasOneClockAndOneAtomicScope(t *testing.T) {
	records := &restoreRecords{}
	repository := &memoryRepository{records: records}
	clockCalls := 0
	service := New(repository, func() time.Time { clockCalls++; return time.UnixMilli(17) })
	if err := service.fenceRestore(t.Context(), "staged.db"); err != nil {
		t.Fatal(err)
	}
	if clockCalls != 1 || !repository.committed || repository.writes != 1 || !reflect.DeepEqual(records.calls, []string{
		"revoke", "reviews", "external", "PEGASUS_IMPORT_ITEM", "EMULATIONSTATION_IMPORT_ITEM", "bulk", "audit",
	}) {
		t.Fatalf("fence escaped atomic scope: %+v %+v", repository, records)
	}
	if records.audit.Now != 17 || records.audit.ID == "" || records.audit.Counts != (FenceCounts{Sessions: 1, Links: 2, Launches: 3, BIOS: 4, Pegasus: 5, EmulationStation: 6}) {
		t.Fatalf("audit evidence: %+v", records.audit)
	}
	records.failAudit = context.Canceled
	if err := service.fenceRestore(t.Context(), "staged.db"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost failure: %v", err)
	}
	if repository.committed {
		t.Fatal("failed audit committed security changes")
	}
}

func TestRestoreDoesNotRevokeAccessBeforeContentValidation(t *testing.T) {
	sum := sha256.Sum256([]byte("original"))
	digest := hex.EncodeToString(sum[:])
	repository := &memoryRepository{records: &restoreRecords{}, snapshot: Snapshot{Blobs: []Blob{{SHA256: digest, SizeBytes: 8}}}}
	service := New(repository, func() time.Time { return time.UnixMilli(17) })
	root := t.TempDir()
	blob := filepath.Join(root, "blobs", "sha256", digest[:2], digest[2:4], digest)
	if err := os.MkdirAll(filepath.Dir(blob), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.validateAndFenceRestore(t.Context(), root); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("corrupt content: %v", err)
	}
	if repository.writes != 0 {
		t.Fatal("corrupt restore reached security mutation")
	}
	repository.inspectErr = context.Canceled
	if err := service.validateAndFenceRestore(t.Context(), root); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost inspection error: %v", err)
	}
	if repository.writes != 0 {
		t.Fatal("cancelled inspection reached security mutation")
	}
}
