package libraryimport

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReviewArcadeParentJobsReadsOnlyQueuedJobsInScheduleOrder(t *testing.T) {
	db := metadataDatabase(t)
	for index, job := range []struct {
		id, kind, state string
		available       int
	}{
		{"parent-late", "REVIEW_ARCADE_PARENT_VALIDATE", "QUEUED", 20},
		{"other-kind", "REVIEW_MULTI_DISC_VALIDATE", "QUEUED", 1},
		{"parent-early", "REVIEW_ARCADE_PARENT_VALIDATE", "QUEUED", 10},
		{"parent-running", "REVIEW_ARCADE_PARENT_VALIDATE", "RUNNING", 1},
	} {
		metadataExec(t, db, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'IMPORT_ITEM',?, ?, ?,1,'{}',1,?,0,4,?,?,?)`,
			job.id, job.id, job.kind, strings.Repeat("a", 63)+string(rune('a'+index)), job.state,
			job.available, job.available, job.available)
	}
	ids, err := NewReviewArcadeParentJobs(db).Queued(t.Context())
	if err != nil || len(ids) != 2 || ids[0] != "parent-early" || ids[1] != "parent-late" {
		t.Fatalf("ids=%#v err=%v", ids, err)
	}
}

func TestReviewArcadeParentJobsPreservesCanceledContext(t *testing.T) {
	db := metadataDatabase(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ids, err := NewReviewArcadeParentJobs(db).Queued(ctx)
	if !errors.Is(err, context.Canceled) || ids != nil {
		t.Fatalf("ids=%#v err=%v", ids, err)
	}
}
