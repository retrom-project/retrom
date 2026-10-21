package storage

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"retrom/internal/model"
)

func TestWriteFailuresRetainCapacityClassification(t *testing.T) {
	cases := []struct {
		cause error
		full  bool
	}{{syscall.ENOSPC, true}, {syscall.EDQUOT, true}, {os.ErrPermission, false}}
	for _, c := range cases {
		err := diskFailure("publish package", &os.PathError{Op: "fsync", Path: "package", Err: c.cause})
		if !errors.Is(err, c.cause) {
			t.Fatalf("lost underlying failure: %v", err)
		}
		full := errors.Is(err, model.ErrStorageFull)
		if full != c.full {
			t.Fatalf("incorrect capacity classification: %v", err)
		}
	}
}
