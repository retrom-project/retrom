package emulationstationimport

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"retrom/internal/repo/store"
	application "retrom/internal/service/emulationstationimport"
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
	return application.CreationPlan{ImportID: "import-" + id, JobID: "job-" + id, ExecutionID: "execution-" + id, AuditID: "audit-" + id, ActorID: "actor", DedupeKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + fmt.Sprintf("%04x", index), Request: application.CreateRequest{RootID: "games", SourceRelativePath: "Roms"}, Root: application.SelectedRoot{ID: "games", Label: "Games", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, ReleaseYearMax: 1971, NowMS: 1, ExpiresAtMS: 604800001}
}

func TestCreationRollsBackWriteAndLateFailures(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"job", "plan", "audit", "after snapshot"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			assertCreationRollback(t, phase)
		})
	}
}

func assertCreationRollback(t *testing.T, phase string) {
	t.Helper()
	db := creationDatabase(t)
	repo := NewCreation(db)
	if err := repo.WithCreate(t.Context(), func(writer application.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(0))
		return err
	}); err != nil {
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
	err := repo.WithCreate(t.Context(), func(writer application.CreationWriter) error {
		value, err := writer.Insert(t.Context(), plan)
		if err != nil {
			return err
		}
		if value.ID != plan.ImportID || value.State != "SCANNING" {
			t.Fatalf("transactional snapshot: %#v", value)
		}
		if phase == "after snapshot" {
			return errCreationWrite
		}
		return nil
	})
	if err == nil {
		t.Fatalf("%s failure committed", phase)
	}
	if phase == "after snapshot" && !errors.Is(err, errCreationWrite) {
		t.Fatalf("lost callback cause: %v", err)
	}
	assertCreationCounts(t, db, 1)
}

func assertCreationCounts(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	for _, query := range []string{`SELECT count(*) FROM emulationstation_imports`, `SELECT count(*) FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT'`, `SELECT count(*) FROM job_input_snapshots`, `SELECT count(*) FROM job_events WHERE scope_type='EMULATIONSTATION_IMPORT'`, `SELECT count(*) FROM audit_events WHERE action='EMULATIONSTATION_IMPORT_CREATED'`} {
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
	repo := NewCreation(db)
	for index := 0; index < 20; index++ {
		err := repo.WithCreate(t.Context(), func(writer application.CreationWriter) error {
			_, err := writer.Insert(t.Context(), creationPlan(index))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	err := repo.WithCreate(t.Context(), func(writer application.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(20))
		return err
	})
	if !errors.Is(err, application.ErrActive) {
		t.Fatalf("capacity: %v", err)
	}
	assertCreationCounts(t, db, 20)
}
