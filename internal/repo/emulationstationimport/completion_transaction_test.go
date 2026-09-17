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

func completionDatabase(t *testing.T) (*sql.DB, emulationstationimportmodel.Execution) {
	t.Helper()
	db, unit := itemWorkDatabase(t)
	item, found, err := itemWorkService(db).Next(t.Context(), unit)
	if err != nil || !found {
		t.Fatalf("item=%v error=%v", found, err)
	}
	if err := itemWorkService(db).Finish(
		t.Context(),
		unit,
		item.ID,
		emulationstationimportmodel.ItemOutcome{State: "READ_FAILED", Code: "READ_FAILED", Retryable: true},
	); err != nil {
		t.Fatal(err)
	}
	return db, unit
}

func completionService(db *sql.DB) *emulationstationimportservice.Completion {
	return emulationstationimportservice.NewCompletion(NewCompletion(db), func() time.Time { return time.UnixMilli(1100) })
}

func TestCompletionPersistsCountsEventAndPayloadTogether(t *testing.T) {
	t.Parallel()
	db, unit := completionDatabase(t)
	if err := completionService(db).Finish(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil || summary.State != "PARTIAL_FAILURE" || !summary.Retryable || summary.Counts.Failed != 1 || summary.Counts.Blocked != 1 || summary.Counts.SkippedMapping != 1 {
		t.Fatalf("summary=%#v error=%v", summary, err)
	}
	assertCompletionPayloadEvent(t, db, unit)

	before := planRows(t, db)
	if err := completionService(db).Finish(t.Context(), unit); !errors.Is(err, emulationstationimportmodel.ErrVersionConflict) {
		t.Fatalf("duplicate completion=%v", err)
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("duplicate completion changed state")
	}
}

func TestCompletionRejectsUnfinishedImportWithoutWrites(t *testing.T) {
	t.Parallel()
	db, unit := itemWorkDatabase(t)
	before := planRows(t, db)
	if err := completionService(db).Finish(t.Context(), unit); !errors.Is(err, emulationstationimportmodel.ErrActive) {
		t.Fatalf("unfinished=%v", err)
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("unfinished completion retained writes")
	}
}

func assertCompletionPayloadEvent(t *testing.T, db *sql.DB, unit emulationstationimportmodel.Execution) {
	t.Helper()
	var state, event, payload string
	var worker, lease sql.NullString
	err := db.QueryRowContext(t.Context(), `SELECT state,worker_id,leased_until_ms,(SELECT json_extract(data_json,'$.state') FROM job_events WHERE job_id=jobs.id AND event_type='SUCCEEDED'),(SELECT payload_state FROM emulationstation_import_items WHERE id='start-item-2') FROM jobs WHERE id=?`, unit.JobID).Scan(

		&state,

		&worker,

		&lease,

		&event,

		&payload,
	)
	if err != nil || state != "SUCCEEDED" || worker.Valid || lease.Valid || event != "PARTIAL_FAILURE" || payload != "RETAINED" {
		t.Fatalf("state=%s worker=%v lease=%v event=%s payload=%s error=%v", state, worker, lease, event, payload, err)
	}
}
