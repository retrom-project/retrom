package files

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func TestRunResourcesExpireBeforeImportStaging(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	staged, err := store.Stage(t.Context(), uuid.NewString(), strings.NewReader("staged member"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.BuildTemporaryPackage(t.Context(), uuid.NewString(), []model.ContentFile{{
		LogicalKey: "one.txt", StorageKey: staged.Key, SHA256: staged.SHA256, SizeBytes: staged.Size,
	}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	age(t, store, staged.Key, now.Add(-3*time.Hour))
	age(t, store, run.StorageKey, now.Add(-3*time.Hour))
	service := &Service{Storage: store, Now: func() time.Time { return now }, Grace: 24 * time.Hour}
	sweepOrphans(t, service)
	if !exists(t, store, staged.Key) || exists(t, store, run.StorageKey) {
		t.Fatal("a long import lost staging, or an idle Run resource survived its lifetime")
	}
	age(t, store, staged.Key, now.Add(-25*time.Hour))
	sweepOrphans(t, service)
	if exists(t, store, staged.Key) {
		t.Fatal("abandoned staging survived the normal grace")
	}
}

func age(t *testing.T, store *storage.Store, key string, at time.Time) {
	t.Helper()
	absolute, err := store.Absolute(key)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(absolute, at, at); err != nil {
		t.Fatal(err)
	}
}

func sweepOrphans(t *testing.T, service *Service) {
	t.Helper()
	for range 8 {
		if err := service.orphans(t.Context()); err != nil {
			t.Fatal(err)
		}
		if service.scanner == nil {
			return
		}
	}
	t.Fatal("orphan scan did not finish")
}

func exists(t *testing.T, store *storage.Store, key string) bool {
	t.Helper()
	file, err := store.Read(key)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return true
}
