package emulationstationimport

import (
	"database/sql"
	"errors"
	"reflect"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"testing"
	"time"
)

func TestPlanDeletionRollsBackEveryProjectionOnFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("late deletion failure")
	for _, phase := range []string{"stale", "audit", "after deletion"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			db, before := planDatabase(t)
			original := planRows(t, db)
			plan := emulationstationimportmodel.PlanDeletion{Before: before, ActorID: "actor", AuditID: "delete-audit", NowMS: 10}
			if phase == "stale" {
				plan.Before.Version++
			}
			if phase == "audit" {
				plan.AuditID = creationPlan(0).AuditID
			}
			err := NewPlanLifecycle(db).WithPlanWrite(t.Context(), func(records emulationstationimportmodel.PlanRecords) error {
				if err := records.Delete(t.Context(), plan); err != nil {
					return err
				}
				return cause
			})
			if err == nil {
				t.Fatal("failed deletion committed")
			}
			if phase == "after deletion" && !errors.Is(err, cause) {
				t.Fatalf("lost deletion cause: %v", err)
			}
			if phase == "stale" && !errors.Is(err, emulationstationimportmodel.ErrInvalid) {
				t.Fatalf("stale deletion cause: %v", err)
			}
			if got := planRows(t, db); !reflect.DeepEqual(got, original) {
				t.Fatalf("partial deletion: before=%v after=%v", original, got)
			}
		})
	}
}

func TestPlanExpiryRollsBackChildrenAndCounts(t *testing.T) {
	t.Parallel()
	cause := errors.New("late expiry failure")
	for _, phase := range []string{"stale", "early", "after expiry"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			db, before := planDatabase(t)
			original := planRows(t, db)
			plan := emulationstationimportmodel.PlanExpiry{Before: before, NowMS: before.ExpiresAtMS}
			if phase == "stale" {
				plan.Before.Version++
			}
			if phase == "early" {
				plan.NowMS--
			}
			err := NewPlanLifecycle(db).WithPlanWrite(t.Context(), func(records emulationstationimportmodel.PlanRecords) error {
				if err := records.Expire(t.Context(), plan); err != nil {
					return err
				}
				return cause
			})
			want := emulationstationimportmodel.ErrInvalid
			if phase == "after expiry" {
				want = cause
			}
			if !errors.Is(err, want) {
				t.Fatalf("expiry phase=%s error=%v", phase, err)
			}
			if got := planRows(t, db); !reflect.DeepEqual(got, original) {
				t.Fatalf("partial expiry: before=%v after=%v", original, got)
			}
		})
	}
}

func TestPlanExpiryUpdatesItemsOnceAtDeadline(t *testing.T) {
	t.Parallel()
	db, before := planDatabase(t)
	now := time.UnixMilli(before.ExpiresAtMS - 1)
	service := emulationstationimportservice.NewPlanLifecycle(NewPlanLifecycle(db), func() time.Time { return now })
	original := planRows(t, db)
	if err := service.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := planRows(t, db); !reflect.DeepEqual(got, original) {
		t.Fatal("plan expired before its deadline")
	}
	now = now.Add(time.Millisecond)
	if err := service.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := NewQueries(db).Get(t.Context(), before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "EXPIRED" || current.Version != before.Version+1 || current.Counts.Cancelled != 1 {
		t.Fatalf("expiry summary=%#v", current)
	}
	assertExpiredPlanItem(t, db, now.UnixMilli())
	terminal := planRows(t, db)
	if err := service.Expire(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := planRows(t, db); !reflect.DeepEqual(got, terminal) {
		t.Fatal("repeated expiry changed durable state")
	}
	assertCreationCounts(t, db, 1)
}

func assertExpiredPlanItem(t *testing.T, db *sql.DB, now int64) {
	t.Helper()
	var state, code, payload string
	var version, completed int64
	if err := db.QueryRowContext(t.Context(), `SELECT execution_state,error_code,version,completed_at_ms,payload_state FROM emulationstation_import_items WHERE id='item'`).Scan(&state, &code, &version, &completed, &payload); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" || code != "EMULATIONSTATION_PLAN_EXPIRED" || version != 2 || completed != now || payload != "RETAINED" {
		t.Fatalf("expiry item=%s %s version=%d completed=%d payload=%s", state, code, version, completed, payload)
	}
}
