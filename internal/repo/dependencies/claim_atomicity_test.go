package dependencies

import (
	"context"
	"database/sql"
	"testing"

	service "retrom/internal/model/dependencies"

	_ "modernc.org/sqlite"
)

func TestDATClaimEventFailureRollsBackState(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	// Omit job_events to inject a write failure without database triggers.
	_, err = database.ExecContext(t.Context(), `
CREATE TABLE jobs (
 id TEXT PRIMARY KEY, state TEXT, attempt_count INTEGER,
 execution_started_at_ms INTEGER, execution_deadline_at_ms INTEGER,
 leased_until_ms INTEGER, heartbeat_at_ms INTEGER, worker_id TEXT,
 version INTEGER, updated_at_ms INTEGER
);
CREATE TABLE dat_versions (id TEXT PRIMARY KEY, parse_status TEXT, version INTEGER, updated_at_ms INTEGER);
INSERT INTO jobs(id,state,attempt_count,version) VALUES('job','QUEUED',0,1);
INSERT INTO dat_versions(id,parse_status,version) VALUES('dat','PENDING',1);
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = New(database).CommitClaimDAT(ctx, service.ClaimDATCommand{
		Claim:   service.JobClaim{JobID: "job", DATID: "dat", AtMS: 1000},
		MarkDAT: "dat",
	})
	if err == nil {
		t.Error("claim succeeded despite missing event storage")
	}
	var state, parseStatus string
	var attempt, jobVersion, datVersion int64
	if err := database.QueryRowContext(t.Context(), "SELECT state,attempt_count,version FROM jobs WHERE id='job'").
		Scan(&state, &attempt, &jobVersion); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), "SELECT parse_status,version FROM dat_versions WHERE id='dat'").
		Scan(&parseStatus, &datVersion); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || attempt != 0 || jobVersion != 1 || parseStatus != "PENDING" || datVersion != 1 {
		t.Fatalf("failed claim leaked state: job=%s attempt=%d version=%d DAT=%s version=%d",
			state, attempt, jobVersion, parseStatus, datVersion)
	}
}
