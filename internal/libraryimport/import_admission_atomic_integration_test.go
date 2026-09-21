//go:build integration

package libraryimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

type admissionNotification struct{ jobs []string }

func (notification *admissionNotification) NotifyImportGroup(_ context.Context, id string) {
	notification.jobs = append(notification.jobs, id)
}

func TestImportAdmissionLateEventFailureRollsBackAllRecords(t *testing.T) {
	t.Parallel()
	service, request := admissionFixture(t)
	before := admissionDatabaseRows(t, service.database)
	cause := errors.New("queue event unavailable")
	pendingWrites, eventHits := int64(0), 0
	faulty := testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO job_events") {
				eventHits++
				return cause
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.Contains(query, "INSERT INTO import_job_files") {
				count, err := result.RowsAffected()
				if err != nil {
					return nil, err
				}
				pendingWrites += count
			}
			return result, nil
		},
	})
	notification := &admissionNotification{}
	admissions := application.NewImportAdmissions(repository.NewImportAdmissions(faulty), notification, service.tags,
		application.ImportAdmissionOptions{Now: service.now})
	result, err := admissions.Queue(t.Context(), request)
	if !errors.Is(err, cause) || result != (Created{}) || eventHits != 1 || pendingWrites != 1 || len(notification.jobs) != 0 {
		t.Fatalf("result=%+v err=%v events=%d writes=%d notify=%v", result, err, eventHits, pendingWrites, notification.jobs)
	}
	after := admissionDatabaseRows(t, service.database)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed admission changed database before=%v after=%v", before, after)
	}
	admissions = application.NewImportAdmissions(repository.NewImportAdmissions(service.database), notification, service.tags,
		application.ImportAdmissionOptions{Now: service.now})
	result, err = admissions.Queue(t.Context(), request)
	if err != nil || result.JobID == "" || len(notification.jobs) != 1 || notification.jobs[0] != result.JobID {
		t.Fatalf("retry result=%+v err=%v notify=%v", result, err, notification.jobs)
	}
	assertAdmittedImportRecords(t, service.database, result)
}

func admissionDatabaseRows(t *testing.T, database *sql.DB) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"jobs", "job_events", "job_input_snapshots", "import_jobs", "import_items", "import_group_requests", "import_job_files", "upload_consumptions", "upload_sessions", "upload_files", "blobs", "blob_gc_candidates"} {
		result[table] = approvalTableRows(t, database, table)
	}
	return result
}

func assertAdmittedImportRecords(t *testing.T, database *sql.DB, result Created) {
	t.Helper()
	var jobs, imports, inputs, requests, files, consumptions, events, items int
	err := database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM jobs WHERE kind='IMPORT_GROUP'),(SELECT count(*) FROM import_jobs),
 (SELECT count(*) FROM job_input_snapshots WHERE job_id=?),(SELECT count(*) FROM import_group_requests),
 (SELECT count(*) FROM import_job_files),(SELECT count(*) FROM upload_consumptions),
 (SELECT count(*) FROM job_events WHERE job_id=?),(SELECT count(*) FROM import_items)`, result.JobID, result.JobID).
		Scan(&jobs, &imports, &inputs, &requests, &files, &consumptions, &events, &items)
	if err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || imports != 1 || inputs != 1 || requests != 1 || files != 1 || consumptions != 1 || events != 1 || items != 0 {
		t.Fatalf("jobs/imports/input/request/files/consumption/events/items=%d/%d/%d/%d/%d/%d/%d/%d", jobs, imports, inputs, requests, files, consumptions, events, items)
	}
}

func TestImportAdmissionAffectedRowsFailureKeepsCause(t *testing.T) {
	t.Parallel()
	service, request := admissionFixture(t)
	before := admissionDatabaseRows(t, service.database)
	cause := errors.New("admission affected rows unavailable")
	hits := 0
	faulty := testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.Contains(query, "UPDATE platform_instances SET version=version") {
				hits++
				return approvalResultFailure{Result: result, cause: cause}, nil
			}
			return result, nil
		},
	})
	admissions := application.NewImportAdmissions(repository.NewImportAdmissions(faulty), nil, service.tags, application.ImportAdmissionOptions{Now: service.now})
	result, err := admissions.Queue(t.Context(), request)
	if !errors.Is(err, cause) || errors.Is(err, ErrVersionConflict) || result != (Created{}) || hits != 1 {
		t.Fatalf("result=%+v err=%v hits=%d", result, err, hits)
	}
	if !reflect.DeepEqual(before, admissionDatabaseRows(t, service.database)) {
		t.Fatal("failed admission changed database")
	}
}

func TestImportAdmissionLostFenceRollsBack(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"upload_sessions", "platform_instances"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			service, request := admissionFixture(t)
			before := admissionDatabaseRows(t, service.database)
			hits := 0
			faulty := testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
				AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
					if strings.Contains(query, "UPDATE "+table+" SET version=version") {
						hits++
						return driver.RowsAffected(0), nil
					}
					return result, nil
				},
			})
			admissions := application.NewImportAdmissions(repository.NewImportAdmissions(faulty), nil, service.tags,
				application.ImportAdmissionOptions{Now: service.now})
			result, err := admissions.Queue(t.Context(), request)
			if !errors.Is(err, ErrVersionConflict) || result != (Created{}) || hits != 1 {
				t.Fatalf("result=%+v err=%v hits=%d", result, err, hits)
			}
			if !reflect.DeepEqual(before, admissionDatabaseRows(t, service.database)) {
				t.Fatal("lost fence changed database")
			}
		})
	}
}
