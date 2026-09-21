package libraryimport

import (
	"context"
	"errors"
	"testing"
)

func TestQueuedJobsFiltersKindAndOrdersByAvailability(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	metadataExec(t, database, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES('queued-late','IMPORT_ITEM','item','REVIEW_MULTI_DISC_VALIDATE',lower(hex(randomblob(32))),1,'{}',1,'QUEUED',0,1,20,1,1),
('queued-early','IMPORT_ITEM','item','REVIEW_MULTI_DISC_VALIDATE',lower(hex(randomblob(32))),1,'{}',1,'QUEUED',0,1,10,1,1),
('running','IMPORT_ITEM','item','REVIEW_MULTI_DISC_VALIDATE',lower(hex(randomblob(32))),1,'{}',1,'RUNNING',0,1,1,1,1),
('other','IMPORT_ITEM','item','REVIEW_ARCADE_PARENT_VALIDATE',lower(hex(randomblob(32))),1,'{}',1,'QUEUED',0,1,1,1,1)`)
	jobs, err := NewQueuedJobs(database).Queued(t.Context(), "REVIEW_MULTI_DISC_VALIDATE")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].ID != "queued-early" || jobs[1].ID != "queued-late" {
		t.Fatalf("jobs=%#v", jobs)
	}
}

func TestQueuedJobsPreservesQueryAndCancellationErrors(t *testing.T) {
	t.Parallel()
	database := metadataDatabase(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	jobs, err := NewQueuedJobs(database).Queued(ctx, "REVIEW_MULTI_DISC_VALIDATE")
	if !errors.Is(err, context.Canceled) || jobs != nil {
		t.Fatalf("jobs=%#v err=%v", jobs, err)
	}
}
