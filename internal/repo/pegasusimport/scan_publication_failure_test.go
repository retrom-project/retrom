package pegasusimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"retrom/internal/repo/dbexec"
	pegasusimportservice "retrom/internal/service/pegasusimport"
	"retrom/internal/testkit/testsupport"

	pegasusimportmodel "retrom/internal/model/pegasusimport"

	"modernc.org/sqlite"
)

func TestScanPublicationChecksAffectedRowsAfterActualInsert(t *testing.T) {
	t.Parallel()
	for _, zero := range []bool{false, true} {
		db, id, projection := publicationDatabase(t)
		before := publicationRows(t, db)
		cause := errors.New("cannot count scanned metadata")
		hits := 0
		fault := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
			AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
				if strings.HasPrefix(
					query,
					"INSERT INTO pegasus_import_metadata_files(",
				) && len(
					args,
				) == 8 && args[0].Value == id.ImportID {
					hits++
					if n, err := result.RowsAffected(); err != nil || n != 1 {
						t.Errorf("insert was not executed: %d %v", n, err)
					}
					if zero {
						return driver.RowsAffected(0), nil
					}
					return materialAffectedFailure{Result: result, cause: cause}, nil
				}
				return result, nil
			},
		})
		service := pegasusimportservice.NewScanPublication(NewScanPublication(fault), func() time.Time { return time.UnixMilli(10) })
		err := service.Headers(t.Context(), id, projection.Headers)
		expected := cause
		if zero {
			expected = pegasusimportmodel.ErrVersionConflict
		}
		if hits != 1 || !errors.Is(err, expected) || !reflect.DeepEqual(before, publicationRows(t, db)) {
			t.Fatalf("partial headers zero=%v hits=%d err=%v", zero, hits, err)
		}
	}
}

func TestScanPublicationEventFailurePreservesStagedSnapshot(t *testing.T) {
	t.Parallel()
	db, id, projection := publicationDatabase(t)
	stagePublication(t, db, id, projection)
	before := publicationRows(t, db)
	cause := errors.New("scan event unavailable")
	hits := 0
	fault := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.HasPrefix(query, "INSERT INTO job_events(") && len(args) == 4 && args[0].Value == id.JobID {
				hits++
				return cause
			}
			return nil
		},
	})
	service := pegasusimportservice.NewScanPublication(NewScanPublication(fault), func() time.Time { return time.UnixMilli(10) })
	err := service.Finish(t.Context(), id, projection.Summary)
	if hits != 1 || !errors.Is(err, cause) || !reflect.DeepEqual(before, publicationRows(t, db)) {
		t.Fatalf("partial scan finalization hits=%d err=%v", hits, err)
	}
}

func TestScanPublicationCommitFailureRollsBackPublishedOutcome(t *testing.T) {
	t.Parallel()
	db, id, projection := publicationDatabase(t)
	stagePublication(t, db, id, projection)
	before := publicationRows(t, db)
	repo := NewScanPublication(db)
	repo.WithPreCommitHook(func(tx dbexec.Executor) error {
		if _, err := tx.ExecContext(t.Context(), `CREATE TABLE scan_commit_failure(
owner TEXT REFERENCES blobs(id) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), `INSERT INTO scan_commit_failure VALUES('missing')`)
		return err
	})
	service := pegasusimportservice.NewScanPublication(repo, func() time.Time { return time.UnixMilli(10) })
	err := service.Finish(t.Context(), id, projection.Summary)
	var cause *sqlite.Error
	if !errors.As(err, &cause) || !reflect.DeepEqual(before, publicationRows(t, db)) {
		t.Fatalf("failed commit changed scan: %v", err)
	}
}
