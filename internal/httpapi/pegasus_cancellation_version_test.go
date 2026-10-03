package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"retrom/internal/composition"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

type beforeSourceCancellation struct {
	service *application.Service
	before  func(application.JobCancellationRequest)
}

func (gate beforeSourceCancellation) CancelJob(
	ctx context.Context,
	request application.JobCancellationRequest,
) (application.JobCancellationResult, bool, error) {
	gate.before(request)
	result, pending, err := gate.service.CancelJob(ctx, request)
	if err != nil {
		return result, pending, fmt.Errorf("cancel after version barrier: %w", err)
	}
	return result, pending, nil
}

func TestJobCancellationUsesLatestExecutionAfterGenericRead(t *testing.T) {
	server := newTestServer(t)
	planID, jobID := seedHTTPSourceScan(t, server, false)
	hits := 0
	server.systemDeps.Jobs = composition.WithSourceJobCancellation(server.systemDeps.Jobs, beforeSourceCancellation{
		service: server.importDeps.Source,
		before: func(request application.JobCancellationRequest) {
			hits++
			if request.ScopeID != planID || request.JobID != jobID || request.ActorID == "" {
				t.Fatalf("dispatcher changed cancellation command: %#v", request)
			}
			if _, err := server.database.ExecContext(
				t.Context(),
				`UPDATE jobs SET version=version+1,execution_no=execution_no+1 WHERE id=?`,
				jobID,
			); err != nil {
				t.Fatal(err)
			}
		},
	})
	response := cancelHTTPScan(t, server, jobID)
	if response.Code != http.StatusOK || hits != 1 {
		t.Fatalf("latest execution cancellation failed: HTTP %d hits=%d", response.Code, hits)
	}
	var planState, jobState string
	var version int64
	err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT plan.state,job.state,job.version
FROM source_imports plan JOIN jobs job ON job.id=plan.scan_job_id WHERE plan.id=?`, planID).Scan(
		&planState,
		&jobState,
		&version,
	)
	if err != nil || planState != "CANCELLED" || jobState != "CANCELLED" || version != 3 {
		t.Fatalf("inconsistent cancellation: %s %s %d %v", planState, jobState, version, err)
	}
}
