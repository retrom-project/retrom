package storage

import (
	"errors"
	"strings"
	"syscall"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
)

func TestStageReportsFreeSpaceInsteadOfFileSize(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := s.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	original := statfs
	t.Cleanup(func() { statfs = original })
	// One block of 16 bytes above the 1 GiB reserve.
	statfs = func(_ string, status *syscall.Statfs_t) error {
		status.Bsize, status.Bavail = 16, (1024*1024*1024+16)/16
		return nil
	}
	if _, err = s.Stage(t.Context(), uuid.NewString(), strings.NewReader("fits")); err != nil {
		t.Fatalf("small file rejected: %v", err)
	}
	_, err = s.Stage(t.Context(), uuid.NewString(), strings.NewReader(strings.Repeat("x", 17)))
	if !errors.Is(err, model.ErrStorageFull) {
		t.Fatalf("free-space limit reported as %v", err)
	}
	_, err = s.StageLimit(t.Context(), uuid.NewString(), strings.NewReader(strings.Repeat("x", 9)), 8)
	if errors.Is(err, model.ErrStorageFull) || !errors.Is(err, errTooLarge) {
		t.Fatalf("size limit reported as %v", err)
	}
}
