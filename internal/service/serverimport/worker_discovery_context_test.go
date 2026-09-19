package serverimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	model "retrom/internal/model/serverimport"
)

func TestDiscoveryStopsCancelledTraversalWithoutCandidateFiles(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		name := "cancelled"
		if expired {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "empty", "nested"), 0o700); err != nil {
				t.Fatal(err)
			}
			directory, err := os.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := directory.Close(); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := context.Canceled
			if expired {
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				cause = context.DeadlineExceeded
				defer cancel()
			} else {
				cancel()
			}
			service := &Service{scanLimits: defaultScanLimits()}
			groups, counts, err := service.discoverCandidates(ctx, model.Work{}, directory, nil)
			if expired && !errors.Is(err, errExecutionDeadline) {
				t.Fatalf("deadline lost worker outcome classification: %v", err)
			}
			if !errors.Is(err, cause) || len(groups) != 0 || counts.Directories != 0 {
				t.Fatalf("cancelled discovery: groups=%d counts=%+v err=%v", len(groups), counts, err)
			}
		})
	}
}
