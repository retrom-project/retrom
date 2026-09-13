//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestMediaRetryDispatchesAfterReceiptAndReplayKeepsExecution(t *testing.T) {
	fixture := newMediaRetryFixture(t)
	response := httptest.NewRecorder()
	fixture.request(t.Context(), response)
	if response.Code != http.StatusAccepted {
		t.Fatalf("retry=%d/%s", response.Code, response.Body.String())
	}
	state, execution, attempt := waitValidationRetry(t, fixture)
	if state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatalf("media retry=%s/%d/%d", state, execution, attempt)
	}
	replay := httptest.NewRecorder()
	fixture.request(t.Context(), replay)
	if replay.Code != http.StatusAccepted || replay.Body.String() != response.Body.String() {
		t.Fatalf("replay=%d/%s", replay.Code, replay.Body.String())
	}
	state, execution, attempt = waitValidationRetry(t, fixture)
	if state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatal("replay created another media execution")
	}
	var assets int
	if err := fixture.server.database.QueryRowContext(t.Context(), `SELECT count(*) FROM scrape_candidate_assets WHERE media_fetch_job_id=? AND status='READY'`, fixture.jobID).Scan(&assets); err != nil {
		t.Fatal(err)
	}
	if assets != 1 {
		t.Fatalf("retried READY assets=%d", assets)
	}
}

func TestMediaRetryReceiptFailureDoesNotInvokeCurrentDispatch(t *testing.T) {
	fixture := newMediaRetryFixture(t)
	hits := 0
	cause := errors.New("media receipt write unavailable")
	fixture.server.database = testsupport.OpenSQLFaultDatabase(t, fixture.server.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO idempotency_records") && len(args) > 1 && args[1].Value == "postAdminJobRetry" {
				hits++
				return cause
			}
			return nil
		},
	})
	response := httptest.NewRecorder()
	fixture.request(t.Context(), response)
	if response.Code != http.StatusInternalServerError || hits != 1 {
		t.Fatalf("receipt=%d/%d", response.Code, hits)
	}
	var state string
	var attempt int64
	if err := fixture.server.database.QueryRowContext(t.Context(), `SELECT state,attempt_count FROM jobs WHERE id=?`, fixture.jobID).Scan(&state, &attempt); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || attempt != 0 {
		t.Fatalf("receipt failure invoked dispatch: %s/%d", state, attempt)
	}
	// The mutation has its own committed transaction. A later durable recovery is still allowed.
	if err := fixture.server.metadata.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, _, attempt = waitValidationRetry(t, fixture)
	if state != "SUCCEEDED" || attempt != 1 {
		t.Fatalf("durable recovery=%s/%d", state, attempt)
	}
}

func TestMediaRetrySurvivesResponseCancellationAndStopsAfterClose(t *testing.T) {
	fixture := newMediaRetryFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := validationCancellingWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	fixture.request(ctx, writer)
	if writer.Code != http.StatusAccepted {
		t.Fatalf("cancel response=%d/%s", writer.Code, writer.Body.String())
	}
	state, execution, attempt := waitValidationRetry(t, fixture)
	if ctx.Err() == nil || state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatalf("cancelled response start=%s/%d/%d", state, execution, attempt)
	}
	fixture.server.metadata.Close()
	if fixture.server.metadata.ResumeMediaJob(t.Context(), fixture.jobID) {
		t.Fatal("media registered after Close")
	}
}
