//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	jobpersistence "retrom/internal/persistence/jobs"
	uploadpersistence "retrom/internal/persistence/uploads"
	"retrom/internal/service/jobs"
	"retrom/internal/service/uploads"
	"retrom/internal/testsupport"
)

func TestUploadFinalizationRetryDispatchesAfterReceiptAndReplayKeepsExecution(t *testing.T) {
	fixture := newUploadRetryFixture(t)
	response := httptest.NewRecorder()
	fixture.request(t.Context(), response)
	if response.Code != http.StatusAccepted {
		t.Fatalf("retry=%d/%s", response.Code, response.Body.String())
	}
	state, execution, attempt := waitValidationRetry(t, fixture.validationRetryFixture)
	if state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatalf("upload retry=%s/%d/%d", state, execution, attempt)
	}
	replay := httptest.NewRecorder()
	fixture.request(t.Context(), replay)
	if replay.Code != http.StatusAccepted || replay.Body.String() != response.Body.String() {
		t.Fatalf("replay=%d/%s", replay.Code, replay.Body.String())
	}
	state, execution, attempt = waitValidationRetry(t, fixture.validationRetryFixture)
	if state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatal("replay created another upload execution")
	}
	var completed int
	if err := dbapi.QueryRowContext(t.Context(), fixture.server.database, `SELECT count(*) FROM upload_sessions WHERE finalize_job_id=? AND state='COMPLETE' AND finalization_no=1`, fixture.jobID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("retried complete uploads=%d", completed)
	}
}

func TestUploadFinalizationRetryReceiptFailureDoesNotInvokeCurrentDispatch(t *testing.T) {
	fixture := newUploadRetryFixture(t)
	hits := 0
	cause := errors.New("upload receipt write unavailable")
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.server.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO idempotency_records") && len(args) > 1 && args[1].Value == "postAdminJobRetry" {
				hits++
				return cause
			}
			return nil
		},
	})
	originalJobs := fixture.server.systemDeps.Jobs
	fixture.server.systemDeps.Jobs = jobs.New(jobpersistence.New(fault), fixture.now)
	response := httptest.NewRecorder()
	fixture.request(t.Context(), response)
	if response.Code != http.StatusInternalServerError || hits != 1 {
		t.Fatalf("receipt=%d/%d", response.Code, hits)
	}
	var state string
	var attempt int64
	if err := dbapi.QueryRowContext(t.Context(), fixture.server.database, `SELECT state,attempt_count FROM jobs WHERE id=?`, fixture.jobID).Scan(&state, &attempt); err != nil {
		t.Fatal(err)
	}
	if state != "FAILED" || attempt != 1 {
		t.Fatalf("receipt failure invoked dispatch: %s/%d", state, attempt)
	}
	assertRetryReceiptRollback(t, fixture.validationRetryFixture, 1)
	if err := fixture.server.importDeps.Uploads.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertRetryReceiptRollback(t, fixture.validationRetryFixture, 1)
	fixture.server.systemDeps.Jobs = originalJobs
	retry := httptest.NewRecorder()
	fixture.request(t.Context(), retry)
	if retry.Code != http.StatusAccepted {
		t.Fatalf("retry after rollback=%d/%s", retry.Code, retry.Body.String())
	}
	state, execution, attempt := waitValidationRetry(t, fixture.validationRetryFixture)
	if state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatalf("retry after rollback=%s/%d/%d", state, execution, attempt)
	}
}

func TestUploadFinalizationRetrySurvivesResponseCancellationAndStopsAfterClose(t *testing.T) {
	fixture := newUploadRetryFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := validationCancellingWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	fixture.request(ctx, writer)
	if writer.Code != http.StatusAccepted {
		t.Fatalf("cancel response=%d/%s", writer.Code, writer.Body.String())
	}
	state, execution, attempt := waitValidationRetry(t, fixture.validationRetryFixture)
	if ctx.Err() == nil || state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatalf("cancelled response start=%s/%d/%d", state, execution, attempt)
	}
	fixture.server.importDeps.Uploads.Close()
	if fixture.server.importDeps.Uploads.Resume(t.Context(), fixture.jobID) {
		t.Fatal("upload registered after Close")
	}
}

func TestUploadCompletePreservesStorageFailureBoundary(t *testing.T) {
	fixture := newUploadRetryFixture(t)
	var uploadID string
	var version int64
	if err := dbapi.QueryRowContext(t.Context(), fixture.server.database, `SELECT id,version FROM upload_sessions WHERE finalize_job_id=?`, fixture.jobID).Scan(&uploadID, &version); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("upload completion read unavailable")
	hits := 0
	database := testsupport.OpenSQLFaultDatabase(t, fixture.server.database, testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
		if strings.Contains(query, "SELECT id,state,version,finalization_no,finalize_job_id") && len(args) == 1 && args[0].Value == uploadID {
			hits++
			return cause
		}
		return nil
	}})
	fixture.server.importDeps.Uploads.Close()
	fixture.server.importDeps.Uploads = uploads.New(uploadpersistence.New(database), nil, t.TempDir(), fixture.now)
	t.Cleanup(fixture.server.importDeps.Uploads.Close)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/uploads/"+uploadID+"/complete", nil)
	request.SetPathValue("uploadId", uploadID)
	request.Header.Set("If-Match", fmt.Sprintf(`"v%d"`, version))
	response := httptest.NewRecorder()
	fixture.server.completeUpload(response, request)
	if hits != 1 || response.Code != http.StatusInternalServerError {
		t.Fatalf("storage failure became conflict: status=%d hits=%d body=%s", response.Code, hits, response.Body.String())
	}
}
