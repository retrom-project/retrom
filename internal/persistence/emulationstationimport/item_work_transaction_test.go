package emulationstationimport

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
)

func itemWorkDatabase(t *testing.T) (*sql.DB, application.Execution) {
	t.Helper()
	db, _ := leaseDatabase(t, true)
	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO emulationstation_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms) VALUES('start-item-2',0,'FILE','item.gba',1,?,'DISCOVERED',1,1)`,
		planDigest,
	); err != nil {
		t.Fatal(err)
	}
	unit, found, err := application.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(
		t.Context(),
	)
	if err != nil || !found {
		t.Fatalf("claim=%v error=%v", found, err)
	}
	return db, unit
}

func itemWorkService(db *sql.DB) *application.ItemWork {
	return application.NewItemWork(NewItemWork(db), func() time.Time { return time.UnixMilli(1100) })
}

func TestItemWorkClaimsResumesAndPersistsAtomicProgress(t *testing.T) {
	t.Parallel()
	db, unit := itemWorkDatabase(t)
	service := itemWorkService(db)
	item, found, err := service.Next(t.Context(), unit)
	if err != nil || !found || item.ID != "start-item-2" || item.State != "COPYING" || item.Version < 2 || len(
		item.Files,
	) != 1 {
		t.Fatalf("item=%#v found=%v error=%v", item, found, err)
	}
	assertItemWorkResume(t, db, service, unit, item)

	err = service.Finish(
		t.Context(),
		unit,
		item.ID,
		application.ItemOutcome{State: "READ_FAILED", Code: "READ_FAILED", Retryable: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertItemWorkOutcome(t, db, unit, item.ID)

	if _, found, err := service.Next(t.Context(), unit); err != nil || found {
		t.Fatalf("finished item repeated: found=%v error=%v", found, err)
	}
}

func assertItemWorkOutcome(t *testing.T, db *sql.DB, unit application.Execution, itemID string) {
	t.Helper()
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil || summary.Counts.Failed != 1 || summary.Counts.Blocked != 1 || summary.Counts.SkippedMapping != 1 {
		t.Fatalf("summary=%#v error=%v", summary, err)
	}
	var state, payload, outcome string
	if err := db.QueryRowContext(t.Context(), `SELECT execution_state,payload_state,(SELECT json_extract(data_json,'$.outcome') FROM job_events WHERE job_id=? AND event_type='PROGRESS') FROM emulationstation_import_items WHERE id=?`, unit.JobID, itemID).Scan(

		&state,

		&payload,

		&outcome,
	); err != nil {
		t.Fatal(err)
	}
	if state != "READ_FAILED" || payload != "RETAINED" || outcome != "READ_FAILED" {
		t.Fatalf("state=%s payload=%s outcome=%s", state, payload, outcome)
	}
}

func assertItemWorkResume(
	t *testing.T,
	db *sql.DB,
	service *application.ItemWork,
	unit application.Execution,
	item application.ExecutionItem,
) {
	t.Helper()
	before := planRows(t, db)
	repeated, found, err := service.Next(t.Context(), unit)
	if err != nil || !found || repeated.ID != item.ID || repeated.Version != item.Version || !reflect.DeepEqual(
		before,
		planRows(t, db),
	) {
		t.Fatalf("resume=%#v found=%v error=%v", repeated, found, err)
	}
}
