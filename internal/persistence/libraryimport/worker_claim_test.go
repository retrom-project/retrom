package libraryimport

import (
	"testing"

	dbapi "retrom/internal/database"
)

func TestAttachmentInvalidInputConsumesAttemptAndKeepsDeadline(t *testing.T) {
	database := attachmentExecutionFixture(t, "REVIEW_ARCADE_PARENT_VALIDATE", "QUEUED")
	metadataExec(t, database, `UPDATE jobs SET attempt_count=0,execution_started_at_ms=NULL,
 execution_deadline_at_ms=NULL,worker_id=NULL,leased_until_ms=NULL WHERE id='parent-job'`)
	_, err := NewArcadeParentAttachmentWorker(database).Claim(t.Context(), "parent-job", "claim-worker", 20)
	if err == nil {
		t.Fatal("missing input unexpectedly claimed")
	}
	var state string
	var attempts int
	var deadline *int64
	if err := dbapi.QueryRowContext(t.Context(), database,
		`SELECT state,attempt_count,execution_deadline_at_ms FROM jobs WHERE id='parent-job'`).
		Scan(&state, &attempts, &deadline); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" || attempts != 1 || deadline == nil {
		t.Fatalf("input failure erased execution budget: state=%s attempts=%d deadline=%v", state, attempts, deadline)
	}
}
