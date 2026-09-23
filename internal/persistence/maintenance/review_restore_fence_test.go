package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"retrom/internal/dbexec"
	application "retrom/internal/service/maintenance"
)

type changedRestoreReview struct {
	application.RestoredReviewRecords
	transaction *sql.Tx
	mutation    string
	changed     bool
}

func (records *changedRestoreReview) Pending(
	ctx context.Context, query application.RestoredReviewQuery,
) ([]application.RestoredReview, error) {
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
		{"plan", `UPDATE source_imports SET version=version+1 WHERE id='import'`},
		{"source", `UPDATE source_import_items SET version=version+1 WHERE id='item'`},
		{"job", `UPDATE jobs SET version=version+1 WHERE id='work'`},
		{"ordinary", `UPDATE import_items SET review_version=review_version+1 WHERE id='handoff-item'`},
		{"owner", `INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id) VALUES('handoff-upload','SOURCE','other')`},
		{"root", `UPDATE source_imports SET root_id='replacement' WHERE id='import'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verifyRestoredReviewFence(t, test.mutation)
		})
	}
}

func verifyRestoredReviewFence(t *testing.T, mutation string) {
	t.Helper()
	db := restoreReviewFixture(t)
	before := reviewRestoreSnapshot(t, db)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	scope := (writes{tx}).Reviews()
	scope.Records = &changedRestoreReview{RestoredReviewRecords: scope.Records, transaction: tx, mutation: mutation}
	err = application.CompleteRestoredReviews(t.Context(), scope, time.UnixMilli(10))
	if !errors.Is(err, application.ErrInvalidBundle) {
		t.Fatalf("restore accepted changed handoff: %v", err)
	}
	var title string
	var events int
	err = tx.QueryRowContext(t.Context(), `SELECT json_extract(metadata_json,'$.title'),
(SELECT review_version-1 FROM import_items WHERE id='handoff-item')
FROM import_items WHERE id='handoff-item'`).Scan(&title, &events)
	wantEvents := 0
	if mutation == `UPDATE import_items SET review_version=review_version+1 WHERE id='handoff-item'` {
		wantEvents = 1
	}
	if err != nil || title != "Original" || events != wantEvents {
		t.Fatalf("fence did not follow actual metadata writes: title=%q events=%d err=%v", title, events, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if before != reviewRestoreSnapshot(t, db) {
		t.Fatal("failed handoff retained earlier metadata or changed ownership")
	}
}

func TestRestoreRejectsSourceReviewOwnedByAnotherSource(t *testing.T) {
	t.Parallel()
	db := restoreReviewFixture(t)
	_, err := db.ExecContext(t.Context(), `INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id)
VALUES('handoff-upload','SOURCE','other-source')`)
	if err != nil {
		t.Fatal(err)
	}
	before := reviewRestoreSnapshot(t, db)
	err = runReviewRestoreTransaction(t.Context(), db)
	if !errors.Is(err, application.ErrInvalidBundle) {
		t.Fatalf("restore accepted another source owner: %v", err)
	}
	if before != reviewRestoreSnapshot(t, db) {
		t.Fatal("invalid owner changed restored records")
	}
}
