package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	application "retrom/internal/service/payloadrelease"
	"retrom/internal/testkit/testsupport"
)

func TestPayloadEffectRejectsReplacedWorkerBeforeAnyRelease(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	claim, found, err := fixture.service.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim: %t/%v", found, err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id=?`, claim.ID); err != nil {
		t.Fatal(err)
	}
	err = fixture.service.execute(t.Context(), claim)
	var payload string
	queryErr := fixture.database.QueryRowContext(t.Context(), `SELECT payload_state FROM games WHERE id='schedule-game'`).Scan(&payload)
	if !errors.Is(err, application.ErrExecutionLost) || queryErr != nil || payload != "RELEASING" {
		t.Fatalf("stale worker changed payload: %s/%v/%v", payload, err, queryErr)
	}
}

func TestPayloadSettlementRollsBackEveryPriorWriteOnEvidenceFailure(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"event", "audit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			fixture := queuedReleaseWorker(t)
			claim, found, err := fixture.service.claim(t.Context())
			if err != nil || !found {
				t.Fatalf("claim: %t/%v", found, err)
			}
			before := releaseJobAuthority(t, fixture.database, claim.ID)
			cause := errors.New("terminal evidence unavailable")
			var hits atomic.Int64
			fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, terminalEvidenceFault(stage, claim.ID, cause, &hits))
			service, err := New(fault, nil, fixture.service.now, 7*24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			err = service.finish(t.Context(), claim, nil)
			if !errors.Is(err, cause) || hits.Load() != 1 || releaseJobAuthority(t, fixture.database, claim.ID) != before {
				t.Fatalf("failed settlement escaped: error=%v hits=%d", err, hits.Load())
			}
			var events, audits int
			err = fixture.database.QueryRowContext(t.Context(), `SELECT
   (SELECT count(*) FROM job_events WHERE job_id=? AND event_type='SUCCEEDED'),
   (SELECT count(*) FROM audit_events WHERE resource_id='schedule-game')`, claim.ID).Scan(&events, &audits)
			if err != nil || events != 0 || audits != 0 {
				t.Fatalf("partial terminal evidence: %d/%d/%v", events, audits, err)
			}
		})
	}
}

func TestPayloadEffectRollsBackWhenLeaseExpiresDuringWrites(t *testing.T) {
	t.Parallel()
	fixture := queuedReleaseWorker(t)
	claim, found, err := fixture.service.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim: %t/%v", found, err)
	}
	var hits atomic.Int64
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			q := strings.Join(strings.Fields(query), " ")
			if strings.HasPrefix(q, "UPDATE games SET payload_state='RELEASED'") {
				for _, arg := range args {
					if arg.Value == "schedule-game" {
						hits.Add(1)
						fixture.now.Store(claim.Work.Lease.Value + 1)
						break
					}
				}
			}
			return result, nil
		},
	})
	service, err := New(fault, nil, fixture.service.now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	err = service.execute(t.Context(), claim)
	var payload string
	queryErr := fixture.database.QueryRowContext(t.Context(), `SELECT payload_state FROM games WHERE id='schedule-game'`).Scan(&payload)
	if !errors.Is(err, application.ErrExecutionLost) || queryErr != nil || payload != "RELEASING" || hits.Load() != 1 {
		t.Fatalf("expired effect escaped its transaction: payload=%s hits=%d error=%v query=%v", payload, hits.Load(), err, queryErr)
	}
}

func terminalEvidenceFault(stage, id string, cause error, hits *atomic.Int64) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{AfterExec: func(_ context.Context, query string, args []driver.NamedValue,
		result driver.Result,
	) (driver.Result, error) {
		prefix := "INSERT INTO job_events"
		bound := id
		if stage == "audit" {
			prefix = "INSERT INTO audit_events"
			bound = "schedule-game"
		}
		if strings.HasPrefix(strings.Join(strings.Fields(query), " "), prefix) {
			for _, arg := range args {
				if arg.Value == bound {
					hits.Add(1)
					return failedSchedulingCount{Result: result, cause: cause}, nil
				}
			}
		}
		return result, nil
	}}
}
