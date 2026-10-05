package dependencies

import (
	"context"
	"testing"

	dbapi "retrom/internal/database"
	dbpostgres "retrom/internal/database/postgres"
	service "retrom/internal/service/dependencies"
	"retrom/internal/testsupport/testpostgres"
)

func TestDATClaimEventFailureRollsBackState(t *testing.T) {
	database, err := dbpostgres.Open(testpostgres.DSN(t), dbpostgres.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	// Omit job_events to inject a write failure without database triggers.
	_, err = database.ExecContext(t.Context(), `
CREATE TABLE jobs (
 id TEXT PRIMARY KEY, state TEXT, attempt_count BIGINT,
 execution_started_at_ms BIGINT, execution_deadline_at_ms BIGINT,
 leased_until_ms BIGINT, heartbeat_at_ms BIGINT, worker_id TEXT,
 version BIGINT, updated_at_ms BIGINT
);
CREATE TABLE dat_versions (id TEXT PRIMARY KEY, parse_status TEXT, version BIGINT, updated_at_ms BIGINT);
INSERT INTO jobs(id,state,attempt_count,version) VALUES('job','QUEUED',0,1);
INSERT INTO dat_versions(id,parse_status,version) VALUES('dat','PENDING',1);
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = New(database).WithWrite(ctx, func(scope service.WriteScope) error {
		if err := scope.Jobs.Claim(ctx, service.JobClaim{JobID: "job", DATID: "dat", AtMS: 1000}); err != nil {
			return err
		}
		return scope.Catalog.MarkParsing(ctx, "dat", 1000)
	})
	if err == nil {
		t.Error("claim succeeded despite missing event storage")
	}
	var state, parseStatus string
	var attempt, jobVersion, datVersion int64
	if err := dbapi.QueryRowContext(t.Context(), database, "SELECT state,attempt_count,version FROM jobs WHERE id='job'").
		Scan(&state, &attempt, &jobVersion); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(t.Context(), database, "SELECT parse_status,version FROM dat_versions WHERE id='dat'").
		Scan(&parseStatus, &datVersion); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || attempt != 0 || jobVersion != 1 || parseStatus != "PENDING" || datVersion != 1 {
		t.Fatalf("failed claim leaked state: job=%s attempt=%d version=%d DAT=%s version=%d",
			state, attempt, jobVersion, parseStatus, datVersion)
	}
}
