package pegasusimport

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	application "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/store"
)

var errCreationWrite = errors.New("creation write failed")

func creationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	owner, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "creation.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := owner.SQL.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile','Admin',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SQL.ExecContext(t.Context(), `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms) VALUES('actor','profile','admin','Admin','ADMIN','ENABLED',1,1)`); err != nil {
		t.Fatal(err)
	}
	return owner.SQL
}

func creationPlan(index int) application.CreationPlan {
	id := strconv.Itoa(index)
	return application.CreationPlan{ImportID: "import-" + id, JobID: "job-" + id, ExecutionID: "execution-" + id, AuditID: "audit-" + id, ActorID: "actor", DedupeKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + fmt.Sprintf("%04x", index), Request: application.CreateRequest{RootID: "games", SourceRelativePath: "Roms"}, Root: application.SelectedRoot{ID: "games", Label: "Games", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, NowMS: 1, ExpiresAtMS: 604800001}
}

func TestCreationRollsBackWriteAndLateFailures(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"job", "plan", "audit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			assertCreationRollback(t, phase)
		})
	}
	t.Run("after snapshot", func(t *testing.T) {
		t.Parallel()
		db := creationDatabase(t)
		if _, err := NewCreation(db).CommitCreation(t.Context(), creationPlan(0)); err != nil {
			t.Fatal(err)
		}
		repo := NewCreation(db)
		repo.WithPreCommitHook(func() error { return errCreationWrite })
		_, err := repo.CommitCreation(t.Context(), creationPlan(1))
		if !errors.Is(err, errCreationWrite) {
			t.Fatalf("lost callback cause: %v", err)
		}
		assertCreationCounts(t, db, 1)
	})
}

func assertCreationRollback(t *testing.T, phase string) {
	t.Helper()
	db := creationDatabase(t)
	if _, err := NewCreation(db).CommitCreation(t.Context(), creationPlan(0)); err != nil {
		t.Fatal(err)
	}
	plan := creationPlan(1)
	switch phase {
	case "job":
		plan.DedupeKey = ""
	case "plan":
		plan.Root.Digest = ""
	case "audit":
		plan.AuditID = creationPlan(0).AuditID
	}
	_, err := NewCreation(db).CommitCreation(t.Context(), plan)
	if err == nil {
		t.Fatalf("%s failure committed", phase)
	}
	assertCreationCounts(t, db, 1)
}

func assertCreationCounts(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	for _, query := range []string{`SELECT count(*) FROM pegasus_imports`, `SELECT count(*) FROM jobs WHERE scope_type='PEGASUS_IMPORT'`, `SELECT count(*) FROM job_input_snapshots`, `SELECT count(*) FROM job_events WHERE scope_type='PEGASUS_IMPORT'`, `SELECT count(*) FROM audit_events WHERE action='PEGASUS_IMPORT_CREATED'`} {
		var got int
		if err := db.QueryRowContext(t.Context(), query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s: got %d, want %d", query, got, want)
		}
	}
}

func TestCreationEnforcesTwentyPlanCapacityAtInsert(t *testing.T) {
	t.Parallel()
	db := creationDatabase(t)
	for index := 0; index < 20; index++ {
		if _, err := NewCreation(db).CommitCreation(t.Context(), creationPlan(index)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := NewCreation(db).CommitCreation(t.Context(), creationPlan(20))
	if !errors.Is(err, application.ErrActive) {
		t.Fatalf("capacity: %v", err)
	}
	assertCreationCounts(t, db, 20)
}
