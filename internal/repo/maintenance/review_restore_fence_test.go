package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	maintenancemodel "retrom/internal/model/maintenance"
	"retrom/internal/repo/dbexec"
	maintenanceservice "retrom/internal/service/maintenance"
)

type changedRestoreReview struct {
	maintenancemodel.RestoredReviewRecords
	transaction *sql.Tx
	mutation    string
	changed     bool
}

func (records *changedRestoreReview) Pending(
	ctx context.Context, query maintenancemodel.RestoredReviewQuery,
) ([]maintenancemodel.RestoredReview, error) {
	result, err := records.RestoredReviewRecords.Pending(ctx, query)
	if err != nil || len(result) == 0 || records.changed {
		return result, err
	}
	records.changed = true
	_, err = records.transaction.ExecContext(ctx, records.mutation)
	return result, err
}

func TestRestoredReviewRechecksFrozenOwnershipBeforeHandoff(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, mutation string }{
		{"plan", `UPDATE emulationstation_imports SET version=version+1 WHERE id='es-import'`},
		{"source", `UPDATE emulationstation_import_items SET version=version+1 WHERE id='es-item'`},
		{"job", `UPDATE jobs SET version=version+1 WHERE id='es-work'`},
		{"ordinary", `UPDATE import_items SET version=version+1 WHERE id='handoff-item'`},
		{"owner", `DELETE FROM server_import_upload_owners WHERE source_item_id='es-item'`},
		{"year", `UPDATE emulationstation_imports SET release_year_max=1972 WHERE id='es-import'`},
		{"root", `UPDATE emulationstation_imports SET root_id='replacement' WHERE id='es-import'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verifyRestoredReviewFence(t, test.mutation)
		})
	}
}

func verifyRestoredReviewFence(t *testing.T, mutation string) {
	t.Helper()
	db := restoreReviewFixture(t, "EMULATIONSTATION")
	before := reviewRestoreSnapshot(t, db)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	scope := (writes{tx}).Reviews()
	scope.Records = &changedRestoreReview{RestoredReviewRecords: scope.Records, transaction: tx, mutation: mutation}
	err = maintenanceservice.CompleteRestoredReviews(t.Context(), scope, time.UnixMilli(10))
	if !errors.Is(err, maintenancemodel.ErrInvalidBundle) {
		t.Fatalf("restore accepted changed handoff: %v", err)
	}
	var title string
	var events int
	err = tx.QueryRowContext(t.Context(), `SELECT json_extract(metadata_json,'$.title'),
(SELECT count(*) FROM review_events WHERE import_item_id='handoff-item')
FROM review_drafts WHERE import_item_id='handoff-item'`).Scan(&title, &events)
	if err != nil || title != "Restored title" || events != 1 {
		t.Fatalf("fence did not follow actual metadata writes: title=%q events=%d err=%v", title, events, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if before != reviewRestoreSnapshot(t, db) {
		t.Fatal("failed handoff retained earlier metadata or changed ownership")
	}
}

func TestRestoreRejectsPegasusReviewOwnedByAnotherSource(t *testing.T) {
	t.Parallel()
	db := restoreReviewFixture(t, "PEGASUS")
	_, err := db.ExecContext(t.Context(), `INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id)
VALUES('handoff-upload','PEGASUS','other-source')`)
	if err != nil {
		t.Fatal(err)
	}
	before := reviewRestoreSnapshot(t, db)
	err = runReviewRestoreTransaction(t.Context(), db)
	if !errors.Is(err, maintenancemodel.ErrInvalidBundle) {
		t.Fatalf("restore accepted another source owner: %v", err)
	}
	if before != reviewRestoreSnapshot(t, db) {
		t.Fatal("invalid owner changed restored records")
	}
}
