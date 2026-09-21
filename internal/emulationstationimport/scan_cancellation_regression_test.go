package emulationstationimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNextESDomainCanCancelQueuedAndRunningScan(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "running"}[running], func(t *testing.T) {
			fixture, created := nextQueuedScan(t)
			if running {
				if _, found, err := fixture.service.claim(fixture.context); err != nil || !found {
					t.Fatalf("scan not claimed: %v", err)
				}
			}
			current, err := fixture.service.Get(fixture.context, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			result, pending, err := fixture.service.Cancel(fixture.context, created.ID, current.Version, "Stop", fixture.userID)
			want := "CANCELLED"
			if running {
				want = "CANCEL_REQUESTED"
			}
			if err != nil || result.State != want || pending != running {
				t.Fatalf("scan cancel state=%s pending=%v error=%v", result.State, pending, err)
			}
		})
	}
}

func TestNextESCancelledEmptyDirectoryScanPreservesCause(t *testing.T) {
	fixture := newLifecycleFixture(t)
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "empty", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(fixture.context)
	cancel()
	_, err := fixture.service.scan(ctx, Root{path: directory}, "", 2027)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled directory-only scan cause=%v", err)
	}
}
