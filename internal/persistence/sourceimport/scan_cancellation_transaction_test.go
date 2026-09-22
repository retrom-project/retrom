package sourceimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	application "retrom/internal/service/sourceimport"
	"retrom/internal/testsupport"
)

func TestScanCancellationRechecksOriginalJobVersionAndOwnership(t *testing.T) {
	t.Parallel()
	db, id, _ := publicationDatabase(t)
	service := application.NewWorkflowControl(NewWorkflowControl(db), func() time.Time { return time.UnixMilli(10) })
	before := publicationRows(t, db)
	if result, pending, err := service.CancelJob(
		t.Context(),
		application.JobCancellationRequest{
			JobID:           id.JobID,
			ScopeID:         id.ImportID,
			Kind:            "IMPORT_SCAN",
			ExpectedVersion: 2,
			Reason:          "Stop",
			ActorID:         "actor",
		},
	); !errors.Is(
		err,
		application.ErrVersionConflict,
	) || pending || result.JobID != "" {
		t.Fatalf("wrong Job version: %#v %v %v", result, pending, err)
	}
	if !reflect.DeepEqual(before, publicationRows(t, db)) {
		t.Fatal("stale cancellation changed rows")
	}
	if result, pending, err := service.CancelJob(
		t.Context(),
		application.JobCancellationRequest{
			JobID:           id.JobID,
			ScopeID:         id.ImportID,
			Kind:            "IMPORT_SCAN",
			ExpectedVersion: 1,
			Reason:          "Stop",
			ActorID:         "actor",
		},
	); err != nil || !pending || result.State != "CANCEL_REQUESTED" {
		t.Fatalf("current scan Job rejected: %#v %v %v", result, pending, err)
	}
}

func TestScanCancellationAuditFailureRollsBackCleanupAndJob(t *testing.T) {
	t.Parallel()
	db, id, projection := publicationDatabase(t)
	stagePublication(t, db, id, projection)
	if _, err := db.ExecContext(t.Context(), `UPDATE jobs SET state='QUEUED',worker_id=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL WHERE id='job-0'`); err != nil {
		t.Fatal(err)
	}
	before := publicationRows(t, db)
	cause := errors.New("scan cancellation audit unavailable")
	deleted, audits := 0, 0
	fault := cancellationAuditFault(t, db, id.ImportID, cause, &deleted, &audits)
	service := application.NewWorkflowControl(NewWorkflowControl(fault), func() time.Time { return time.UnixMilli(10) })
	result, pending, err := service.CancelJob(
		t.Context(),
		application.JobCancellationRequest{
			JobID:           id.JobID,
			ScopeID:         id.ImportID,
			Kind:            "IMPORT_SCAN",
			ExpectedVersion: 1,
			Reason:          "Stop",
			ActorID:         "actor",
		},
	)
	if !errors.Is(err, cause) || result.JobID != "" || pending || deleted != 1 || audits != 1 {
		t.Fatalf("partial cancel result=%#v pending=%v delete=%d audit=%d err=%v", result, pending, deleted, audits, err)
	}
	if !reflect.DeepEqual(before, publicationRows(t, db)) {
		t.Fatal("failed cancellation consumed scan projection")
	}
}

func TestScanCancellationRepositoryRejectsReplacedParentAndJobSnapshot(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"job", "plan", "execution"} {
		t.Run(change, func(t *testing.T) {
			db, id, _ := publicationDatabase(t)
			before := publicationRows(t, db)
			err := NewWorkflowControl(db).WithControl(t.Context(), func(scope application.WorkflowScope) error {
				current, err := scope.Read.CurrentJob(t.Context(), id.JobID)
				if err != nil {
					return err
				}
				switch change {
				case "job":
					current.JobVersion++
				case "plan":
					current.Summary.Version++
				default:
					current.Execution++
				}
				return scope.Write.Cancel(t.Context(), application.CancellationPlan{
					Before: current, State: "CANCEL_REQUESTED",
					Pending: true, Reason: "Stop", ActorID: "actor", AuditID: "cancel-audit", NowMS: 10,
				})
			})
			if !errors.Is(err, application.ErrNotCancellable) || !reflect.DeepEqual(before, publicationRows(t, db)) {
				t.Fatalf("stale %s changed state: %v", change, err)
			}
		})
	}
}

func cancellationAuditFault(t *testing.T, db *sql.DB, importID string, cause error, deleted, audits *int) *sql.DB {
	t.Helper()
	return testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if query == "DELETE FROM source_import_items WHERE import_id=?" && len(args) == 1 && args[0].Value == importID {
				if count, err := result.RowsAffected(); err != nil || count != 1 {
					t.Errorf("cleanup did not execute: %d %v", count, err)
				}
				*deleted++
			}
			return result, nil
		},
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.HasPrefix(
				strings.TrimSpace(query),
				"INSERT INTO audit_events(",
			) && len(
				args,
			) == 5 && args[3].Value == importID {
				*audits++
				return cause
			}
			return nil
		},
	})
}
