package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/testsupport"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func TestAttachmentClaimPreservesExecutionBudgetAndCapsLease(t *testing.T) {
	for _, kind := range []string{"REVIEW_ARCADE_PARENT_VALIDATE", "REVIEW_MULTI_DISC_VALIDATE"} {
		t.Run(kind, func(t *testing.T) {
			database := attachmentExecutionFixture(t, kind, "RUNNING")
			metadataExec(t, database, `UPDATE jobs SET state='QUEUED',worker_id=NULL,leased_until_ms=NULL WHERE id='parent-job'`)
			tx, err := database.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer dbapi.Rollback(tx)
			if kind == "REVIEW_ARCADE_PARENT_VALIDATE" {
				err = claimArcadeParentAttachmentRecords(t.Context(), tx, "parent-job", "new-worker", 20)
			} else {
				err = claimMultiDiscAttachmentRecords(t.Context(), tx, "parent-job", "new-worker", 20)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var attempt, execution, deadline, lease int64
			if err := dbapi.QueryRowContext(t.Context(), database, `SELECT attempt_count,execution_no,execution_deadline_at_ms,leased_until_ms FROM jobs WHERE id='parent-job'`).Scan(&attempt, &execution, &deadline, &lease); err != nil {
				t.Fatal(err)
			}
			if attempt != 2 || execution != 1 || deadline != 2000 || lease != 2000 {
				t.Fatalf("claim reset execution budget: attempt=%d execution=%d deadline=%d lease=%d", attempt, execution, deadline, lease)
			}
		})
	}
}

func TestAttachmentAcceptanceRequiresCurrentLeaseAndUnexpiredDeadline(t *testing.T) {
	for _, kind := range []string{"REVIEW_ARCADE_PARENT_VALIDATE", "REVIEW_MULTI_DISC_VALIDATE"} {
		t.Run(kind, func(t *testing.T) {
			database := attachmentExecutionFixture(t, kind, "RUNNING")
			metadataExec(t, database, `UPDATE jobs SET execution_deadline_at_ms=20,leased_until_ms=100 WHERE id='parent-job'`)

			domainReads := 0
			fault := testsupport.OpenSQLFaultDatabase(t, database, testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
				if strings.Contains(query, "FROM import_items") {
					domainReads++
					return errors.New("expired worker reached domain reads")
				}
				return nil
			}})
			var err error
			if kind == "REVIEW_ARCADE_PARENT_VALIDATE" {
				err = NewArcadeParentCommitRepository(fault).CommitAccepted(t.Context(), libraryservice.ArcadeParentAcceptedCommit{JobID: "parent-job", WorkerID: "worker", NowMS: 20})
			} else {
				err = NewMultiDiscAttachmentFinalization(fault).WithCommit(t.Context(), func(scope libraryservice.MultiDiscAttachmentCommitScope) error {
					return scope.CommitAccepted(t.Context(), libraryservice.MultiDiscAttachmentCommitWrite{MultiDiscAttachmentCommitRequest: libraryservice.MultiDiscAttachmentCommitRequest{JobID: "parent-job", WorkerID: "worker"}, NowMS: 20})
				})
			}
			if !errors.Is(err, libraryservice.ErrInvalid) || domainReads != 0 {
				t.Fatalf("expired worker passed owner fence: domainReads=%d err=%v", domainReads, err)
			}
			var events, snapshots int
			if err := dbapi.QueryRowContext(t.Context(), database, `SELECT (SELECT count(*) FROM job_events),(SELECT count(*) FROM import_item_source_snapshots)`).Scan(&events, &snapshots); err != nil {
				t.Fatal(err)
			}
			if events != 0 || snapshots != 1 {
				t.Fatalf("expired success wrote artifacts: events=%d snapshots=%d", events, snapshots)
			}
		})
	}
}
