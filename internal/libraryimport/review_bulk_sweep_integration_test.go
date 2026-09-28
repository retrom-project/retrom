//go:build integration

package libraryimport

import (
	"context"
	"testing"
	"time"

	"retrom/internal/authn"
	librarycomposition "retrom/internal/composition/libraryimport"
	dbapi "retrom/internal/database"
	librarypersistence "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"

	"github.com/google/uuid"
)

func TestReviewBulkResumesProgressAfterGamePublication(t *testing.T) {
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "atomic-bulk", "bulk transaction content", 1)
	itemID := created.Items[0].ItemID
	profileID, actorID := uuid.NewString(), uuid.NewString()
	fixture.execute(t, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Bulk Admin',1)`, profileID)
	fixture.execute(t, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,'Bulk Admin','ADMIN','ENABLED',1,1)`, actorID, profileID, "bulk-"+actorID[:8])
	bulkID, jobID := uuid.NewString(), uuid.NewString()
	err := librarypersistence.NewTransactions(fixture.database).Write(fixture.ctx, func(executor dbapi.Executor) error {
		_, createErr := librarypersistence.BindReviewBulkWrites(executor).CreateGlobal(
			fixture.ctx, bulkID, jobID, actorID, time.Now().UnixMilli())
		return createErr
	})
	if err != nil {
		t.Fatal(err)
	}
	const workerID = "bulk-worker"
	err = librarypersistence.NewTransactions(fixture.database).Write(fixture.ctx, func(executor dbapi.Executor) error {
		_, _, claimErr := librarypersistence.BindReviewBulkWorker(executor).Claim(fixture.ctx,
			bulkID, workerID, time.Now().UnixMilli())
		return claimErr
	})
	if err != nil {
		t.Fatal(err)
	}
	var version int64
	var validationID, snapshotID string
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database,
		`SELECT review_version,selected_validation_id,effective_source_snapshot_id FROM import_items WHERE id=?`, itemID).
		Scan(&version, &validationID, &snapshotID); err != nil {
		t.Fatal(err)
	}
	ctx := authn.WithPrincipal(fixture.ctx, authn.Principal{UserID: actorID, ProfileID: profileID, Role: "ADMIN"})
	approve := func(worker string) error {
		_, err := fixture.service.approvals.Approve(ctx, libraryservice.ReviewApprovalRequest{
			ItemID: itemID, ExpectedVersion: version,
			Bulk: &libraryservice.BulkPublicationIntent{
				BulkID: bulkID, JobID: jobID, WorkerID: worker,
				ValidationID: validationID, SourceSnapshotID: snapshotID,
			},
		})
		return err
	}

	if err := approve("stale-worker"); err == nil {
		t.Fatal("stale worker published item")
	}
	var games, published, scanned int
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT
(SELECT count(*) FROM games),published_count,scanned_count FROM review_bulk_approvals WHERE id=?`, bulkID).
		Scan(&games, &published, &scanned); err != nil {
		t.Fatal(err)
	}
	if games != 0 || published != 0 || scanned != 0 {
		t.Fatalf("rollback games=%d published=%d scanned=%d", games, published, scanned)
	}
	if err := approve(workerID); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT
(SELECT count(*) FROM games),published_count,scanned_count FROM review_bulk_approvals WHERE id=?`, bulkID).
		Scan(&games, &published, &scanned); err != nil {
		t.Fatal(err)
	}
	if games != 1 || published != 0 || scanned != 0 {
		t.Fatalf("commit games=%d published=%d scanned=%d", games, published, scanned)
	}
	bulkService := librarycomposition.NewReviewBulk(fixture.database, fixture.service.approvals, time.Now)
	t.Cleanup(bulkService.Close)
	if err := bulkService.Start(context.WithoutCancel(ctx)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		summary, readErr := bulkService.Get(ctx, bulkID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if summary.State == "COMPLETED" {
			break
		}
		if summary.State == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("resume=%#v", summary)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := dbapi.QueryRowContext(ctx, fixture.database, `SELECT count(*) FROM games`).Scan(&games); err != nil {
		t.Fatal(err)
	}
	if games != 1 {
		t.Fatalf("recovery duplicated game: %d", games)
	}
}

func TestReviewBulkSkipsItemEditedAfterCreationAndRecoversProgress(t *testing.T) {
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "edited-after-start", "bulk changed item", 1)
	itemID := created.Items[0].ItemID
	profileID, actorID := uuid.NewString(), uuid.NewString()
	fixture.execute(t, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Bulk Admin',1)`, profileID)
	fixture.execute(t, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,?,'Bulk Admin','ADMIN','ENABLED',1,1)`, actorID, profileID, "bulk-"+actorID[:8])
	bulkID, jobID := uuid.NewString(), uuid.NewString()
	createdAt := time.Now().UnixMilli()
	err := librarypersistence.NewTransactions(fixture.database).Write(fixture.ctx, func(executor dbapi.Executor) error {
		_, createErr := librarypersistence.BindReviewBulkWrites(executor).CreateGlobal(fixture.ctx,
			bulkID, jobID, actorID, createdAt)
		return createErr
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.execute(t, `UPDATE import_items SET review_version=review_version+1,review_updated_at_ms=? WHERE id=?`, createdAt+1, itemID)
	bulkService := librarycomposition.NewReviewBulk(fixture.database, fixture.service.approvals, time.Now)
	t.Cleanup(bulkService.Close)
	if err := bulkService.Start(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		summary, readErr := bulkService.Get(fixture.ctx, bulkID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if summary.State == "COMPLETED" {
			if summary.ScannedCount != 1 || summary.SkippedChangedCount != 1 || summary.PublishedCount != 0 {
				t.Fatalf("summary=%#v", summary)
			}
			break
		}
		if summary.State == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("task did not recover: %#v", summary)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var itemState string
	var games int
	if err := dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT state,(SELECT count(*) FROM games) FROM import_items WHERE id=?`, itemID).Scan(&itemState, &games); err != nil {
		t.Fatal(err)
	}
	if itemState != "REVIEW_PENDING" || games != 0 {
		t.Fatalf("state=%s games=%d", itemState, games)
	}
}
