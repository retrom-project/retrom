package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func seedHTTPSourceScan(t *testing.T, server *Server, running bool) (string, string) {
	t.Helper()
	server.sourceImports.Close()
	planID, jobID := uuid.NewString(), uuid.NewString()
	var actorID string
	if err := server.database.QueryRowContext(t.Context(), `SELECT id FROM users WHERE role='ADMIN' LIMIT 1`).Scan(
		&actorID,
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	_, err := server.database.ExecContext(
		t.Context(),
		`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,
payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'SOURCE_IMPORT',?,'IMPORT_SCAN',?,1,'{}',1,'QUEUED',0,4,?,?,?)`,
		jobID,
		planID,
		strings.Repeat("e", 64),
		now+3600000,
		now,
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.database.ExecContext(
		t.Context(),
		`INSERT INTO source_imports(id,root_id,root_label_snapshot,
source_relative_path,root_config_digest,state,scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES(?,'games','Games','',?,'SCANNING',?,?,?,?,?)`,
		planID,
		strings.Repeat("a", 64),
		jobID,
		actorID,
		now,
		now,
		now+604800000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if running {
		if _, err := server.database.ExecContext(
			t.Context(),
			`UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='scanner',
leased_until_ms=?,heartbeat_at_ms=?,execution_started_at_ms=?,execution_deadline_at_ms=? WHERE id=?`,

			now+60000,
			now,
			now,
			now+3600000,
			jobID,
		); err != nil {
			t.Fatal(err)
		}
	}
	return planID, jobID
}

func cancelHTTPScan(t *testing.T, server *Server, jobID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/jobs/"+jobID+"/cancel",
		strings.NewReader(`{"reason":"Stop scan"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	request.Header.Set("If-Match", `"v1"`)
	cookie, csrf := testSessionCredentials()
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func TestJobHTTPScanCancellationChangesSourcePlanInSameCommit(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprintf("running=%v", running), func(t *testing.T) {
			server := newTestServer(t)
			planID, jobID := seedHTTPSourceScan(t, server, running)
			response := cancelHTTPScan(t, server, jobID)
			expectedState, status := "CANCELLED", http.StatusOK
			if running {
				expectedState, status = "CANCEL_REQUESTED", http.StatusAccepted
			}
			if response.Code != status {
				t.Fatalf("cancel HTTP=%d %s", response.Code, response.Body.String())
			}
			var planState, jobState string
			var jobVersion int64
			err := server.database.QueryRowContext(t.Context(), `SELECT plan.state,job.state,job.version
FROM source_imports plan JOIN jobs job ON job.id=plan.scan_job_id WHERE plan.id=?`, planID).Scan(
				&planState,
				&jobState,
				&jobVersion,
			)
			if err != nil || planState != expectedState || jobState != expectedState || jobVersion != 2 {
				t.Fatalf("inconsistent scan cancel plan=%s job=%s v=%d err=%v", planState, jobState, jobVersion, err)
			}
			if response.Header().Get("ETag") != `"v2"` {
				t.Fatalf("cancel ETag=%s", response.Header().Get("ETag"))
			}
		})
	}
}
