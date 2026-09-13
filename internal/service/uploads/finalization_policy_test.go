package uploads

import (
	"context"
	"errors"
	"testing"
)

func TestFinalizationFailureDistinguishesExecutionAndCallerDeadline(t *testing.T) {
	for _, test := range []struct {
		name          string
		cause         error
		deadline, now int64
		code          string
		retry         bool
	}{
		{"caller deadline", context.DeadlineExceeded, 200, 100, "UPLOAD_FINALIZE_IO", true},
		{"execution deadline", context.DeadlineExceeded, 100, 100, "UPLOAD_FINALIZE_TIMEOUT", true},
		{"close", ErrWorkerClosed, 200, 100, "UPLOAD_FINALIZE_IO", true},
		{"bad part", errors.Join(errPartCorrupt, context.Canceled), 200, 100, "UPLOAD_PART_CORRUPT", false},
		{"invalid input", ErrInputInvalid, 200, 100, "UPLOAD_INPUT_INVALID", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, retry := finalizationFailure(test.cause, test.deadline, test.now)
			if code != test.code || retry != test.retry {
				t.Fatalf("failure=%s/%v", code, retry)
			}
		})
	}
}
