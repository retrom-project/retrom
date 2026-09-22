package sourceimport

import (
	"testing"
	"time"
)

func TestDeleteMappedPlanRemovesTagRelationsAndPreservesTag(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	seedQueryCollection(t, db)
	for _, statement := range []string{
		`UPDATE source_imports SET state='AWAITING_MAPPING',import_job_id=NULL,completed_at_ms=NULL WHERE id='import'`,
		`INSERT INTO tags(id,name,name_key,search_text,status,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms) VALUES('019b0000-0000-7000-8000-000000000001','Tag','tag','tag','ACTIVE','user','user',1,1)`,
		`INSERT INTO source_collection_tags(collection_id,tag_id,assigned_by_user_id,created_at_ms) VALUES('collection','019b0000-0000-7000-8000-000000000001','user',1)`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	if err := service.Delete(t.Context(), "import", 4); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`SELECT count(*) FROM source_imports`, `SELECT count(*) FROM source_import_collections`, `SELECT count(*) FROM source_collection_tags`} {
		var count int
		if err := db.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("remaining projection: %s=%d", query, count)
		}
	}
	var version int64
	if err := db.QueryRowContext(t.Context(), `SELECT version FROM tags WHERE id='019b0000-0000-7000-8000-000000000001'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("tag relationship version=%d", version)
	}
	var audits int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE action='SOURCE_IMPORT_DELETED' AND actor_user_id='user' AND resource_id='import'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("deletion audit count=%d", audits)
	}
	var jobs int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 2 {
		t.Fatalf("immutable job evidence changed: %d", jobs)
	}
}
