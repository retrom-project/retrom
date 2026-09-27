package filedeletion_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"retrom/internal/composition/cleanupjobs"
	"retrom/internal/persistence/filecatalog"
	"retrom/internal/persistence/fileownership"
)

func TestWorkerDeletesOnlyRetiredOwnersIndependentFiles(t *testing.T) {
	db, files, old, _ := fileDeletionRaceFixture(t)
	other, err := files.Put(bytes.NewBufferString("concurrent identical publication"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filecatalog.EnsureRecord(t.Context(), db, other, "application/octet-stream", 10); err != nil {
		t.Fatal(err)
	}
	if err := fileownership.Adopt(t.Context(), db, other.ID, fileownership.Owner{Kind: "GAME", ID: "other"}); err != nil {
		t.Fatal(err)
	}
	worker, err := cleanupjobs.New(t.Context(), db, files, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	if did, err := worker.RunOnce(t.Context()); err != nil || !did {
		t.Fatalf("run=%v %v", did, err)
	}
	if _, err := os.Stat(old.Path); !os.IsNotExist(err) {
		t.Fatalf("retired file not removed: %v", err)
	}
	if content, err := os.ReadFile(other.Path); err != nil || string(content) != "concurrent identical publication" {
		t.Fatalf("other owner damaged: %q %v", content, err)
	}
	if did, err := worker.RunOnce(t.Context()); err != nil || did {
		t.Fatalf("unretired owner was queued: %v %v", did, err)
	}
}
