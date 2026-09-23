package libraryimport

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

func TestReviewBulkConcurrentCreationCommitsOnlyOneJob(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	db.SetMaxOpenConns(1)
	ids := [][2]string{
		{"019b0000-0000-7000-8000-000000000030", "019b0000-0000-7000-8000-000000000031"},
		{"019b0000-0000-7000-8000-000000000032", "019b0000-0000-7000-8000-000000000033"},
	}
	var wait sync.WaitGroup
	results := make(chan error, len(ids))
	for _, pair := range ids {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- NewTransactions(db).Write(t.Context(), func(executor dbexec.Executor) error {
				_, err := BindReviewBulkWrites(executor).CreateGlobal(t.Context(), pair[0], pair[1], "actor", 10)
				return err
			})
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	var tasks, jobs int
	if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM review_bulk_approvals),
(SELECT count(*) FROM jobs WHERE kind='REVIEW_BULK_APPROVE')`).Scan(&tasks, &jobs); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || tasks != 1 || jobs != 1 {
		t.Fatalf("successful creates=%d tasks=%d jobs=%d", successes, tasks, jobs)
	}
}

func TestReviewBulkCreateBoundAndActiveSummary(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	const bulkID = "019b0000-0000-7000-8000-000000000010"
	const jobID = "019b0000-0000-7000-8000-000000000011"
	writes := BindReviewBulkWrites(db)
	created, err := writes.CreateGlobal(t.Context(), bulkID, jobID, "actor", 10)
	if err != nil || created.InitialPendingCount != 1 || created.MaxItemID != "item" {
		t.Fatalf("created=%#v err=%v", created, err)
	}
	_, err = writes.CreateGlobal(t.Context(), "019b0000-0000-7000-8000-000000000012", "019b0000-0000-7000-8000-000000000013", "actor", 11)
	if err == nil {
		t.Fatal("second active task was accepted")
	}
	queries := BindReviewBulkQueries(db)
	active, found, err := queries.ActiveSummary(t.Context())
	if err != nil || !found || active.BulkApprovalID != bulkID {
		t.Fatalf("active=%#v found=%v err=%v", active, found, err)
	}
	summary, err := queries.Summary(t.Context(), bulkID)
	if err != nil || summary.InitialPendingCount != 1 || summary.ScannedCount != 0 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
}

func TestReviewBulkWorkerPersistsCursorAndResumes(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	const bulkID = "019b0000-0000-7000-8000-000000000020"
	const jobID = "019b0000-0000-7000-8000-000000000021"
	if _, err := BindReviewBulkWrites(db).CreateGlobal(t.Context(), bulkID, jobID, "actor", 10); err != nil {
		t.Fatal(err)
	}
	worker := BindReviewBulkWorker(db)
	gotJob, user, err := worker.Claim(t.Context(), bulkID, "worker-1", 11)
	if err != nil || gotJob != jobID || user != "actor" {
		t.Fatalf("claim=%s %s %v", gotJob, user, err)
	}
	item, found, err := worker.Next(t.Context(), bulkID, "worker-1")
	if err != nil || !found || item.ID != "item" {
		t.Fatalf("item=%#v found=%v err=%v", item, found, err)
	}
	if err := worker.Skip(t.Context(), bulkID, jobID, "worker-1", item.ID, "CHANGED", 12); err != nil {
		t.Fatal(err)
	}
	assertReviewBulkResume(t, db, worker, bulkID, jobID)
}

func assertReviewBulkResume(t *testing.T, db *sql.DB, worker *ReviewBulkWorker, bulkID, jobID string) {
	t.Helper()
	ids, err := worker.Resume(t.Context(), 13)
	if err != nil || len(ids) != 1 || ids[0] != bulkID {
		t.Fatalf("resume=%v err=%v", ids, err)
	}
	if _, _, err := worker.Claim(t.Context(), bulkID, "worker-2", 14); err != nil {
		t.Fatal(err)
	}
	_, found, err := worker.Next(t.Context(), bulkID, "worker-2")
	if err != nil || found {
		t.Fatalf("repeated item: found=%v err=%v", found, err)
	}
	if err := worker.Finish(t.Context(), bulkID, jobID, "worker-2", 15); err != nil {
		t.Fatal(err)
	}
	summary, err := BindReviewBulkQueries(db).Summary(t.Context(), bulkID)
	if err != nil || summary.State != "COMPLETED" || summary.ScannedCount != 1 || summary.SkippedChangedCount != 1 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
	if _, _, err := worker.Claim(t.Context(), bulkID, "worker-3", 16); !errors.Is(err, ErrReviewBulkNotRunnable) {
		t.Fatalf("reclaim=%v", err)
	}
}

func TestReviewBulkCandidateQueryRequiresLimit(t *testing.T) {
	_, _, err := reviewBulkCandidateStatement(application.ReviewBulkCandidateQuery{})
	if !errors.Is(err, application.ErrReviewBulkQuery) {
		t.Fatalf("err=%v", err)
	}
}
