package pegasusimport

import (
	"errors"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func TestPlanDeletionRollsBackTagVersionAndMutableProjectionOnAuditFailure(t *testing.T) {
	t.Parallel()
	db := creationDatabase(t)
	created := creationPlan(0)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
		_, err := writer.Insert(t.Context(), created)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE pegasus_imports SET state='AWAITING_MAPPING' WHERE id='import-0'`,
		`INSERT INTO pegasus_import_collections(id,import_id,metadata_relative_path,segment_ordinal,name,game_count,created_at_ms,updated_at_ms) VALUES('collection','import-0','metadata.pegasus.txt',0,'Collection',0,1,1)`,
		`INSERT INTO tags(id,name,name_key,search_text,status,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms) VALUES('019b0000-0000-7000-8000-000000000001','Tag','tag','tag','ACTIVE','actor','actor',1,1)`,
		`INSERT INTO pegasus_collection_tags(collection_id,tag_id,assigned_by_user_id,created_at_ms) VALUES('collection','019b0000-0000-7000-8000-000000000001','actor',1)`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	err := NewPlanLifecycle(db).WithPlanWrite(t.Context(), func(records pegasusimportmodel.PlanRecords) error {
		before, err := records.Get(t.Context(), created.ImportID)
		if err != nil {
			return err
		}
		return records.Delete(t.Context(), pegasusimportmodel.PlanDeletion{Before: before, ActorID: "actor", AuditID: created.AuditID, NowMS: 10})
	})
	if err == nil {
		t.Fatal("duplicate deletion audit committed")
	}
	assertCreationCounts(t, db, 1)
	var version, relations, collections int
	if err := db.QueryRowContext(t.Context(), `SELECT (SELECT version FROM tags),(SELECT count(*) FROM pegasus_collection_tags),(SELECT count(*) FROM pegasus_import_collections)`).Scan(&version, &relations, &collections); err != nil {
		t.Fatal(err)
	}
	if version != 1 || relations != 1 || collections != 1 {
		t.Fatalf("partial deletion: tag version=%d, relations=%d, collections=%d", version, relations, collections)
	}
}

func TestPlanExpiryRejectsStaleVersionAndRollsBackLateFailure(t *testing.T) {
	t.Parallel()
	db := creationDatabase(t)
	plan := creationPlan(0)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
		_, err := writer.Insert(t.Context(), plan)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_imports SET state='AWAITING_MAPPING' WHERE id='import-0'`); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("late expiry failure")
	for _, stale := range []bool{true, false} {
		err := NewPlanLifecycle(db).WithPlanWrite(t.Context(), func(records pegasusimportmodel.PlanRecords) error {
			before, err := records.Get(t.Context(), plan.ImportID)
			if err != nil {
				return err
			}
			if stale {
				before.Version++
			}
			if err := records.Expire(t.Context(), pegasusimportmodel.PlanExpiry{Before: before, NowMS: plan.ExpiresAtMS}); err != nil {
				return err
			}
			return cause
		})
		want := cause
		if stale {
			want = pegasusimportmodel.ErrInvalid
		}
		if !errors.Is(err, want) {
			t.Fatalf("expiry error: %v", err)
		}
	}
	value, err := NewQueries(db).Get(t.Context(), plan.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	if value.State != "AWAITING_MAPPING" || value.Version != 1 || value.CompletedAtMS != nil {
		t.Fatalf("partial expiry: %#v", value)
	}
}

func TestExpiryCommitsAtDeadlineAndDoesNotRepeat(t *testing.T) {
	t.Parallel()
	db := creationDatabase(t)
	plan := creationPlan(0)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer pegasusimportmodel.CreationWriter) error {
		_, err := writer.Insert(t.Context(), plan)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_imports SET state='AWAITING_MAPPING' WHERE id='import-0'`); err != nil {
		t.Fatal(err)
	}
	service := application.NewPlanLifecycle(NewPlanLifecycle(db), func() time.Time { return time.UnixMilli(plan.ExpiresAtMS) })
	for range 2 {
		if err := service.Expire(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	value, err := NewQueries(db).Get(t.Context(), plan.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	if value.State != "EXPIRED" || value.Version != 2 || value.Phase != nil {
		t.Fatalf("expiry projection: %#v", value)
	}
	if value.CompletedAtMS == nil || *value.CompletedAtMS != plan.ExpiresAtMS {
		t.Fatalf("expiry completion: %#v", value.CompletedAtMS)
	}
	if value.LastErrorCode == nil || *value.LastErrorCode != "PEGASUS_PLAN_EXPIRED" {
		t.Fatalf("expiry reason: %#v", value.LastErrorCode)
	}
}
