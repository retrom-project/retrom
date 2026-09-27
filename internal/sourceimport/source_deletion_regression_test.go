package sourceimport

import (
	"errors"
	"os"
	"testing"
	"time"

	"retrom/internal/composition/cleanupjobs"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	reviewpersistence "retrom/internal/persistence/libraryimport"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sourceimport/sourcerelease"
	application "retrom/internal/service/cleanupjobs"
	review "retrom/internal/service/libraryimport"
)

func assertPendingReviewSurvivesSourceDeletion(t *testing.T, database dbapi.DB, files *filestore.Store) {
	t.Helper()
	sourceIDs := ownedSourceTestFiles(t, database, "SOURCE_IMPORT_ITEM")
	reviewIDs := ownedSourceTestFiles(t, database, "IMPORT_ITEM")
	if len(sourceIDs) == 0 || len(reviewIDs) == 0 {
		t.Fatal("expected independent Source and review files before cleanup")
	}
	releases, err := cleanupjobs.New(t.Context(), database, files, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releases.Close)
	for attempt := 0; attempt < 64; attempt++ {
		if err := releases.ReconcileDeletion(t.Context()); err != nil {
			t.Fatal(err)
		}
		worked, err := releases.RunOnce(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	assertSourceAndReviewBytes(t, files, sourceIDs, reviewIDs)
	assertOwnedReviewSourceProjection(t, database)

	var sourceFiles, pendingReviews, transportHistory, failedJobs int
	err = dbapi.QueryRowContext(t.Context(), database, `SELECT
 (SELECT count(*) FROM stored_files WHERE owner_kind='SOURCE_IMPORT_ITEM'),
 (SELECT count(*) FROM import_items WHERE state='REVIEW_PENDING' AND payload_state='RETAINED'),
 (SELECT count(*) FROM upload_files u JOIN import_files i ON i.id=u.id
  WHERE u.state='PURGED' AND u.final_blob_id IS NULL AND i.blob_id IS NULL),
 (SELECT count(*) FROM jobs WHERE kind IN ('OWNER_CLEANUP','FILE_DELETE') AND state='FAILED')`).
		Scan(&sourceFiles, &pendingReviews, &transportHistory, &failedJobs)
	if err != nil || sourceFiles != 0 || pendingReviews != 2 || transportHistory != len(sourceIDs) || failedJobs != 0 {
		t.Fatalf("Source deletion: files=%d reviews=%d history=%d failed=%d error=%v",
			sourceFiles, pendingReviews, transportHistory, failedJobs, err)
	}
}

func ownedSourceTestFiles(t *testing.T, database dbapi.DB, kind string) []string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `SELECT id FROM stored_files WHERE owner_kind=?`, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestSourceRetirementRollsBackReceivedInputsOnOwnerWriteFailure(t *testing.T) {
	service, _, _ := handoffFixture(t)
	seedHandoffMedia(t, service)
	database := service.database
	mustExecSourceTest(t.Context(), t, database, `INSERT INTO upload_files
 (id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
 VALUES('received-source','handoff-upload','rom.gba',4,4,'handoff-media','COMPLETE',1,1);
 INSERT INTO import_files(id,upload_session_id,relative_path,blob_id,size_bytes,created_at_ms)
 VALUES('received-source','handoff-upload','rom.gba','handoff-media',4,1)`)
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(transaction)
	_, err = sourcerelease.Change(t.Context(), transaction, recordstore.Update{
		Set: "payload_state='INVALID'", Scope: recordstore.Scope{Where: "id=?", Args: []any{"item"}},
	}, application.EffectOwnerChange{
		Before: application.EffectOwner{
			Owner: application.Owner{
				Scope: application.Scope{Type: application.ScopeSourceImportItem, ID: "item"},
				State: "VALIDATING", PublicID: "handoff-item",
			}, ParentID: "import",
		}, Released: true, NowMS: 100,
	})
	if err == nil {
		t.Fatal("invalid Source state must reject the final owner write")
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	var complete, retained int
	err = dbapi.QueryRowContext(t.Context(), database, `SELECT
 (SELECT count(*) FROM upload_files u JOIN import_files i ON i.id=u.id
  WHERE u.id='received-source' AND u.state='COMPLETE' AND u.final_blob_id=i.blob_id
  AND u.payload_released_at_ms IS NULL AND i.released_at_ms IS NULL),
 (SELECT count(*) FROM stored_files WHERE id='handoff-media' AND retired_at_ms IS NULL)`).Scan(&complete, &retained)
	if err != nil || complete != 1 || retained != 1 {
		t.Fatalf("failed Source release committed transport or retirement: complete=%d retained=%d error=%v", complete, retained, err)
	}
}

func assertSourceAndReviewBytes(t *testing.T, files *filestore.Store, sourceIDs, reviewIDs []string) {
	t.Helper()
	for _, id := range sourceIDs {
		if _, err := os.Stat(files.Path(id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("released Source bytes remain: %s: %v", id, err)
		}
	}
	for _, id := range reviewIDs {
		if _, err := os.ReadFile(files.Path(id)); err != nil {
			t.Fatalf("pending review lost its independent bytes: %v", err)
		}
	}
}

func readOwnedReviewSources(t *testing.T, database dbapi.DB, snapshot string) ([]review.ReviewSourceRecord, error) {
	t.Helper()
	var result []review.ReviewSourceRecord
	err := reviewpersistence.NewReviewDetail(database).WithRead(t.Context(), func(scope review.ReviewReadScope) error {
		var err error
		result, err = scope.Sources.Files(t.Context(), snapshot)
		return err
	})
	return result, err
}

func ownedReviewSnapshots(t *testing.T, database dbapi.DB) []string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `SELECT effective_source_snapshot_id FROM import_items WHERE state='REVIEW_PENDING'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertOwnedReviewSourceProjection(t *testing.T, database dbapi.DB) {
	t.Helper()
	for _, snapshot := range ownedReviewSnapshots(t, database) {
		received, err := readOwnedReviewSources(t, database, snapshot)
		if err != nil || len(received) != 1 || received[0].Name == "" || received[0].SHA256 == "" {
			t.Fatalf("review source projection lost owned file after Source deletion: files=%#v error=%v", received, err)
		}
	}
}
