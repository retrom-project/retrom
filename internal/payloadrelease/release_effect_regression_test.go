package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

func TestReleaseGameRemainingReadPreservesStorageCause(t *testing.T) {
	fixture := queuedReleaseWorker(t)
	claim := claimEffect(t, fixture)
	cause := errors.New("remaining references unavailable")
	var hits atomic.Int64
	service := effectFaultService(t, fixture, gameRemainingFault(cause, &hits))
	err := service.execute(t.Context(), claim)
	if !errors.Is(err, cause) || hits.Load() != 1 {
		t.Fatalf("remaining storage cause lost: %v hits=%d", err, hits.Load())
	}
	assertEffectRetained(t, fixture, ScopeGame)
}

func TestReleaseConsumptionReadPreservesStorageCause(t *testing.T) {
	fixture := queuedConsumptionEffect(t)
	claim := claimEffect(t, fixture)
	cause := errors.New("consumption snapshot unavailable")
	var hits atomic.Int64
	service := effectFaultService(
		t,
		fixture,
		testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			q := strings.Join(strings.Fields(query), " ")
			if strings.HasPrefix(
				q,
				"SELECT version,released_at_ms,upload_session_id FROM upload_consumptions",
			) && effectBound(
				args,
				"effect-consumption",
			) {
				hits.Add(1)
				return cause
			}
			return nil
		}},
	)
	err := service.execute(t.Context(), claim)
	if !errors.Is(err, cause) || hits.Load() != 1 {
		t.Fatalf("consumption storage cause lost: %v hits=%d", err, hits.Load())
	}
	assertEffectRetained(t, fixture, ScopeUploadConsumption)
}

func TestReleaseGameCompletionZeroCountRollsBack(t *testing.T) {
	fixture := queuedReleaseWorker(t)
	claim := claimEffect(t, fixture)
	var hits atomic.Int64
	service := effectFaultService(
		t,
		fixture,
		testsupport.SQLFaultHooks{
			AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
				q := strings.Join(strings.Fields(query), " ")
				if strings.HasPrefix(q, "UPDATE games SET payload_state='RELEASED'") && effectBound(args, "schedule-game") {
					hits.Add(1)
					return driver.RowsAffected(0), nil
				}
				return result, nil
			},
		},
	)
	err := service.execute(t.Context(), claim)
	if err == nil || hits.Load() != 1 {
		t.Fatalf("zero final count committed: error=%v hits=%d", err, hits.Load())
	}
	assertEffectRetained(t, fixture, ScopeGame)
}

func TestReleaseConsumptionChecksAffectedCount(t *testing.T) {
	for _, failed := range []bool{true, false} {
		t.Run(map[bool]string{true: "count-cause", false: "zero-count"}[failed], func(t *testing.T) {
			fixture := queuedConsumptionEffect(t)
			claim := claimEffect(t, fixture)
			cause := errors.New("consumption affected count unavailable")
			var hits atomic.Int64
			service := effectFaultService(
				t,
				fixture,
				testsupport.SQLFaultHooks{
					AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
						q := strings.Join(strings.Fields(query), " ")
						if strings.HasPrefix(
							q,
							"UPDATE upload_consumptions SET released_at_ms=",
						) && effectBound(
							args,
							"effect-consumption",
						) {
							hits.Add(1)
							if failed {
								return failedSchedulingCount{Result: result, cause: cause}, nil
							}
							return driver.RowsAffected(0), nil
						}
						return result, nil
					},
				},
			)
			err := service.execute(t.Context(), claim)
			if err == nil || hits.Load() != 1 || failed && !errors.Is(err, cause) {
				t.Fatalf("unchecked count committed: error=%v hits=%d", err, hits.Load())
			}
			assertEffectRetained(t, fixture, ScopeUploadConsumption)
		})
	}
}
