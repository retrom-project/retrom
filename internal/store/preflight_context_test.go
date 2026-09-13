package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type preflightContext struct {
	name   string
	ctx    context.Context
	cancel context.CancelFunc
	want   error
}

func TestPreflightPreservesCancellationInsteadOfReportingInvalidSchema(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "preflight.db")
	database, err := Open(t.Context(), path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []preflightContext{
		cancelledPreflightContext(), expiredPreflightContext(),
	} {
		t.Run(test.name, func(t *testing.T) {
			defer test.cancel()
			err := preflightExistingDatabase(test.ctx, path)
			if !errors.Is(err, test.want) || errors.Is(err, ErrSchemaInvalid) {
				t.Fatalf("preflight error=%v, want %v without a schema failure", err, test.want)
			}
		})
	}
}

func cancelledPreflightContext() preflightContext {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return preflightContext{"cancelled", ctx, cancel, context.Canceled}
}

func expiredPreflightContext() preflightContext {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	return preflightContext{"deadline", ctx, cancel, context.DeadlineExceeded}
}
