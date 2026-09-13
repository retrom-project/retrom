package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"retrom/internal/bootstrap/composition"
	application "retrom/internal/service/emulationstationimport"
)

type beforeEmulationStationCancellation struct {
	service *application.Service
	before  func(application.JobCancellationRequest)
}

func (gate beforeEmulationStationCancellation) CancelJob(
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

func TestESJobCancellationRetainsOriginalETagAfterGenericRead(t *testing.T) {
	server := newTestServer(t)
	planID, jobID := seedHTTPEmulationStationScan(t, server, false)
	hits := 0
	server.jobService = composition.WithEmulationStationJobCancellation(server.jobService, beforeEmulationStationCancellation{
		service: server.emulationStationImports,
		before: func(request application.JobCancellationRequest) {
			hits++
			if request.ExpectedVersion != 1 || request.ScopeID != planID || request.JobID != jobID || request.ActorID == "" {
				t.Fatalf("dispatcher changed cancellation command: %#v", request)
			}
			if _, err := server.database.ExecContext(
				t.Context(),
				`UPDATE jobs SET version=version+1 WHERE id=?`,
				jobID,
			); err != nil {
				t.Fatal(err)
			}
		},
	})
	response := cancelHTTPScan(t, server, jobID)
	if response.Code != http.StatusConflict || hits != 1 {
		t.Fatalf("stale Job ETag accepted: HTTP %d hits=%d", response.Code, hits)
	}
	var planState, jobState string
	var version int64
	err := server.database.QueryRowContext(t.Context(), `SELECT plan.state,job.state,job.version
FROM emulationstation_imports plan JOIN jobs job ON job.id=plan.scan_job_id WHERE plan.id=?`, planID).Scan(
		&planState,
		&jobState,
		&version,
	)
	if err != nil || planState != "SCANNING" || jobState != "QUEUED" || version != 2 {
		t.Fatalf("stale dispatch wrote: %s %s %d %v", planState, jobState, version, err)
	}
}

func TestESGenericCancellationUsesJobVersionWhenPlanVersionDiffers(t *testing.T) {
	server := newTestServer(t)
	planID, jobID := seedHTTPEmulationStationScan(t, server, false)
	if _, err := server.database.ExecContext(t.Context(), `UPDATE emulationstation_imports SET version=7 WHERE id=?`, planID); err != nil {
		t.Fatal(err)
	}
	response := cancelHTTPScan(t, server, jobID)
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"v2"` {
		t.Fatalf("cancel=%d %s ETag=%s", response.Code, response.Body.String(), response.Header().Get("ETag"))
	}
	var version int
	if err := server.database.QueryRowContext(t.Context(), `SELECT version FROM emulationstation_imports WHERE id=?`, planID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 8 {
		t.Fatalf("plan version=%d", version)
	}
}
