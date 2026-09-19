package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	"retrom/internal/testkit/testsupport"
)

func TestReleaseGraphChecksAuthorityAfterGCStaging(t *testing.T) {
	for _, boundary := range []string{"lease", "deadline", "cancel"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := queuedReleaseWorker(t)
			seedEffectGamePayload(t, fixture.database)
			claim := claimEffect(t, fixture)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var hits atomic.Int64
			service := effectFaultService(t, fixture, testsupport.SQLFaultHooks{AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
				if strings.HasPrefix(strings.TrimSpace(query), "INSERT INTO blob_gc_candidates") && effectBound(args, "effect-blob") {
					hits.Add(1)
					switch boundary {
					case "lease":
						fixture.now.Store(claim.Work.Lease.Value)
					case "deadline":
						fixture.now.Store(claim.Work.Deadline.Value)
					case "cancel":
						cancel()
					}
				}
				return result, nil
			}})
			err := service.execute(ctx, claim)
			expected := payloadreleasemodel.ErrExecutionLost

			if boundary == "cancel" {
				expected = context.Canceled
			}
			if !errors.Is(err, expected) || hits.Load() != 1 {
				t.Fatalf("late %s error=%v hits=%d", boundary, err, hits.Load())
			}
			assertEffectGameGraphRetained(t, fixture)
		})
	}
}
