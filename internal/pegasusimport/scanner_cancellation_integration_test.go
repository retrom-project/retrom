package pegasusimport

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestScannerExecutionCancellationStopsReaderAndKeepsOwnership(t *testing.T) {
	t.Parallel()
	for _, replacement := range []bool{false, true} {
		name := "cancel"
		if replacement {
			name = "replacement"
		}
		t.Run(name, func(t *testing.T) {
			service, unit, _ := scanPublicationFixture(t)
			root := scannerBoundarySource(t)
			service.roots = map[string]Root{"games": root}
			unit.RootID, unit.RootDigest = "games", root.digest
			entered := make(chan struct{})
			var acquisitions atomic.Int64
			service.sourceReader = func(ctx context.Context) (func(), error) {
				if acquisitions.Add(1) == 1 {
					return func() {}, nil
				}
				close(entered)
				<-ctx.Done()
				return nil, fmt.Errorf("blocked metadata reader: %w", ctx.Err())
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); service.execute(ctx, unit) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("metadata reader was never acquired")
			}
			if replacement {
				mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id='scan'`)
				service.signal()
			} else {
				before, err := service.Get(t.Context(), unit.ImportID)
				if err != nil {
					t.Fatal(err)
				}
				_, pending, err := service.Cancel(t.Context(), unit.ImportID, before.Version, "Stop metadata scan", "user")
				if err != nil || !pending {
					t.Fatalf("request live cancellation: %v %v", pending, err)
				}
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("metadata reader survived durable owner change")
			}
			assertCancelledScannerOwnership(t, service, replacement)
			assertEmptyScanProjection(t, service)
		})
	}
}

func assertCancelledScannerOwnership(t *testing.T, service *Service, replacement bool) {
	t.Helper()
	var state, owner, plan string
	err := service.database.QueryRowContext(t.Context(), `SELECT j.state,COALESCE(j.worker_id,''),p.state FROM jobs j
 JOIN pegasus_imports p ON p.scan_job_id=j.id WHERE j.id='scan'`).Scan(&state, &owner, &plan)
	if err != nil {
		t.Fatal(err)
	}
	if replacement {
		if state != "RUNNING" || owner != "replacement" || plan != "SCANNING" {
			t.Fatalf("old worker wrote replacement: %s %s %s", state, owner, plan)
		}
	} else if state != "CANCELLED" || owner != "" || plan != "CANCELLED" {
		t.Fatalf("scan cancellation not settled: %s %s %s", state, owner, plan)
	}
}
