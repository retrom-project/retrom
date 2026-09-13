package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	jobpersistence "retrom/internal/repo/jobs"
	"retrom/internal/service/jobs"
	"retrom/internal/testkit/testsupport"
)

func TestJobCancellationSQLFailureIsAnInfrastructureError(t *testing.T) {
	server := newTestServer(t)
	_, jobID := seedHTTPPegasusScan(t, server, false)
	cause := errors.New("job cancellation read failed")
	hits := 0
	fault := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "FROM jobs WHERE id=?") && len(args) == 1 && args[0].Value == jobID {
				hits++
				return cause
			}
			return nil
		},
	})
	server.jobService = jobs.New(jobpersistence.New(fault), time.Now)
	response := cancelHTTPScan(t, server, jobID)
	if response.Code != http.StatusInternalServerError || hits != 1 {
		t.Fatalf("job SQL failure=%d hits=%d body=%s", response.Code, hits, response.Body.String())
	}
}
