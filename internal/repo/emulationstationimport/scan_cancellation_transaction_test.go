package emulationstationimport

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

func insertScanPlan(t *testing.T, db *sql.DB, index int) emulationstationimportmodel.Summary {
	t.Helper()
	var summary emulationstationimportmodel.Summary
	err := NewCreation(db).WithCreate(t.Context(), func(writer emulationstationimportmodel.CreationWriter) error {
		var err error
		summary, err = writer.Insert(t.Context(), creationPlan(index))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func scanCancellationService(db *sql.DB) *emulationstationimportservice.WorkflowControl {
	return emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), nil, func() time.Time { return time.UnixMilli(1001) })
}

func TestRunningScanCancellationDoesNotCompeteWithActiveImport(t *testing.T) {
	t.Parallel()
	db, active := workflowDatabase(t, false)
	scan := insertScanPlan(t, db, 1)
	unit, found, err := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
	if err != nil || !found || unit.JobID != scan.ScanJobID {
		t.Fatalf("claim=%#v found=%v err=%v", unit, found, err)
	}
	scan, err = NewQueries(db).Get(t.Context(), scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, pending, err := scanCancellationService(db).Cancel(t.Context(), scan.ID, scan.Version, "Stop scan", "actor")
	if err != nil || !pending || result.State != "CANCEL_REQUESTED" {
		t.Fatalf("cancel=%#v pending=%v err=%v", result, pending, err)
	}
	after, err := NewQueries(db).Get(t.Context(), active.ID)
	if err != nil || after.State != active.State || after.Version != active.Version {
		t.Fatalf("active changed=%#v err=%v", after, err)
	}
}

func TestPendingScanCancellationRetainsPlanCapacity(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"read", "insert"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			assertPendingScanCapacity(t, stage)
		})
	}
}

func TestPendingScanCancellationDoesNotBlockStartOrRetry(t *testing.T) {
	t.Parallel()
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "retry"}[retry], func(t *testing.T) {
			t.Parallel()
			assertPendingScanAllowsExecution(t, retry)
		})
	}
}

func assertPendingScanCapacity(t *testing.T, stage string) {
	t.Helper()
	db, unit, _ := scanDatabase(t)
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	if _, pending, err := scanCancellationService(db).Cancel(t.Context(), summary.ID, summary.Version, "Stop", "actor"); err != nil || !pending {
		t.Fatalf("cancel pending=%v err=%v", pending, err)
	}
	for index := 1; index < 20; index++ {
		insertScanPlan(t, db, index)
	}
	err = NewCreation(db).WithCreate(t.Context(), func(writer emulationstationimportmodel.CreationWriter) error {
		if stage == "read" {
			count, err := writer.PendingPlans(t.Context())
			if err != nil {
				return err
			}
			if count != 20 {
				t.Fatalf("pending capacity=%d", count)
			}
			return nil
		}
		_, err := writer.Insert(t.Context(), creationPlan(20))
		return err
	})
	if stage == "insert" && !errors.Is(err, emulationstationimportmodel.ErrActive) {
		t.Fatalf("capacity cause=%v", err)
	}
	if stage == "read" && err != nil {
		t.Fatal(err)
	}
}

func assertPendingScanAllowsExecution(t *testing.T, retry bool) {
	t.Helper()
	var db *sql.DB
	var summary emulationstationimportmodel.Summary
	if retry {
		db, summary = workflowDatabase(t, true)
	} else {
		db = startDatabase(t)
		var err error
		summary, err = NewQueries(db).Get(t.Context(), "import-0")
		if err != nil {
			t.Fatal(err)
		}
	}
	pendingScan := insertScanPlan(t, db, 1)
	unit, found, err := emulationstationimportservice.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
	if err != nil || !found || unit.JobID != pendingScan.ScanJobID {
		t.Fatalf("claim=%#v found=%v err=%v", unit, found, err)
	}
	pendingScan, err = NewQueries(db).Get(t.Context(), pendingScan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, pending, err := scanCancellationService(db).Cancel(t.Context(), pendingScan.ID, pendingScan.Version, "Stop", "actor"); err != nil || !pending {
		t.Fatalf("cancel pending=%v err=%v", pending, err)
	}
	if retry {
		_, err = emulationstationimportservice.NewWorkflowControl(NewWorkflowControl(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(1002) }).Retry(t.Context(), summary.ID, summary.Version, mappingActor)
	} else {
		_, _, err = emulationstationimportservice.NewStarter(NewStarter(db), verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(1002) }).Start(t.Context(), summary.ID, summary.Version, mappingActor)
	}
	if err != nil {
		t.Fatalf("scan cancellation blocked new execution: %v", err)
	}
}
