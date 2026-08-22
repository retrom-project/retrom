package processlock

import (
	"errors"
	"testing"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestExclusiveLock(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	first, err := Acquire(directory)
	testassert.Falsef(t, err != nil, "first acquire: %v", err)
	t.Cleanup(func() { cleanup.Error("close", first.Close()) })
	if _, err := Acquire(directory); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquire error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release: %v", err)
	}
	second, err := Acquire(directory)
	testassert.Falsef(t, err != nil, "acquire after release: %v", err)
	cleanup.Error("close", second.Close())
}
