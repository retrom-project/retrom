package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/bootstrap/composition"
	esrepository "retrom/internal/repo/emulationstationimport"
	es "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestESGenericCancellationStorageFailureReturns500AndRollsBack(t *testing.T) {
	server := newTestServer(t)
	planID, jobID := seedHTTPEmulationStationScan(t, server, false)
	var hits atomic.Int64
	failure := errors.New("ES cancellation storage unavailable")
	faultDB := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
		if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), "UPDATE jobs SET state=") {
			return nil
		}
		for _, arg := range args {
			if arg.Value == jobID {
				hits.Add(1)
				return failure
			}
		}
		return nil
	}})
	server.jobService = composition.WithEmulationStationJobCancellation(server.jobService, es.NewWorkflowControl(esrepository.NewWorkflowControl(faultDB), nil, time.Now))
	response := cancelHTTPScan(t, server, jobID)
	if response.Code != http.StatusInternalServerError || hits.Load() != 1 {
		t.Fatalf("cancel=%d hits=%d %s", response.Code, hits.Load(), response.Body.String())
	}
	var planState, jobState string
	var version int
	err := server.database.QueryRowContext(t.Context(), `SELECT plan.state,job.state,job.version FROM emulationstation_imports plan JOIN jobs job ON job.id=plan.scan_job_id WHERE plan.id=?`, planID).Scan(&planState, &jobState, &version)
	if err != nil || planState != "SCANNING" || jobState != "QUEUED" || version != 1 {
		t.Fatalf("partial cancel=%s/%s v=%d error=%v", planState, jobState, version, err)
	}
}
