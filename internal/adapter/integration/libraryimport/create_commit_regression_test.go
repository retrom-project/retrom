//go:build integration

package libraryimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func TestImportCreationPreservesCommitReadCause(t *testing.T) {
	for _, table := range []string{"upload_sessions", "platform_instances"} {
		t.Run(table, func(t *testing.T) {
			service, plan := preparedCommitFixture(t)
			cause := errors.New("creation input unavailable")
			reads := 0
			service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if strings.Contains(query, "FROM "+table) {
						reads++
						return cause
					}
					return nil
				},
			})
			before := creationEffectCounts(t, service.database)
			created, err := commitPreparedFixture(t.Context(), service, plan, nil)
			if !errors.Is(err, cause) || created != (Created{}) || reads != 1 {
				t.Fatalf("input read: result=%+v reads=%d error=%v", created, reads, err)
			}
			assertCreationEffectsUnchanged(t, service.database, before)
		})
	}
}

func TestImportCreationRejectsStaleQueuedExecution(t *testing.T) {
	for _, change := range []struct{ name, statement string }{
		{"execution", `UPDATE jobs SET execution_no=execution_no+1 WHERE id=?`},
		{"attempt", `UPDATE jobs SET attempt_count=attempt_count+1 WHERE id=?`},
		{"lease", `UPDATE jobs SET leased_until_ms=1 WHERE id=?`},
		{"deadline", `UPDATE jobs SET execution_deadline_at_ms=1 WHERE id=?`},
	} {
		t.Run(change.name, func(t *testing.T) {
			service, plan := preparedCommitFixture(t)
			admissions := libraryimportservice.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil,
				service.tags, libraryimportmodel.ImportAdmissionOptions{Now: service.now})
			created, err := admissions.Queue(t.Context(), plan.Request)
			if err != nil {
				t.Fatal(err)
			}
			work, err := service.claimImportGroup(t.Context(), created.JobID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.database.ExecContext(t.Context(), change.statement, work.jobID); err != nil {
				t.Fatal(err)
			}
			before := creationEffectCounts(t, service.database)
			result, err := commitPreparedFixture(t.Context(), service, plan, &work)
			if err == nil || result != (Created{}) {
				t.Fatalf("stale %s committed: result=%+v error=%v", change.name, result, err)
			}
			assertCreationEffectsUnchanged(t, service.database, before)
			var state string
			if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, work.jobID).
				Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != "RUNNING" {
				t.Fatalf("stale work changed job state to %s", state)
			}
		})
	}
}

func preparedCommitFixture(t *testing.T) (*Service, creationPlan) {
	t.Helper()
	database, blobs, directory := openImportGroupFixture(t, t.Context())
	uploadID := completeImportGroupUpload(t, t.Context(), database.SQL, blobs, directory, onsProjectArchive(t))
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	plan, err := service.prepareCreation(t.Context(), onsImportGroupRequest(t, database.SQL, uploadID))
	if err != nil {
		t.Fatal(err)
	}
	return service, plan
}

func commitPreparedFixture(
	ctx context.Context, service *Service, plan creationPlan, work *queuedCreationWork,
) (Created, error) {
	options := libraryimportmodel.ImportCreationOptions{}
	if work != nil {
		options.Queued = work.creationIntent()
	}
	result, err := service.importCreations().CommitPrepared(ctx, plan, options)
	return result.Created, err
}

func creationEffectCounts(t *testing.T, database *sql.DB) map[string]int64 {
	t.Helper()
	result := make(map[string]int64)
	for _, table := range []string{
		"jobs", "job_events", "import_jobs", "import_job_files", "upload_consumptions", "archive_entries",
		"import_items", "review_drafts", "import_item_source_files", "import_item_source_snapshots",
		"import_item_source_snapshot_files", "import_item_core_validations", "import_item_validation_files",
		"import_item_dos_entries", "import_item_multidisc_entries", "import_item_duplicate_matches",
	} {
		var count int64
		if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		result[table] = count
	}
	return result
}

func assertCreationEffectsUnchanged(t *testing.T, database *sql.DB, before map[string]int64) {
	t.Helper()
	for table, count := range creationEffectCounts(t, database) {
		if count != before[table] {
			t.Errorf("%s count=%d, before creation=%d", table, count, before[table])
		}
	}
}
