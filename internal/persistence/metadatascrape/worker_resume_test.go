package metadatascrape

import (
	"context"
	"strings"
	"testing"
	"time"

	"retrom/internal/hasheous"
	"retrom/internal/service/metadatascrape"
)

func TestMetadataReattemptUsesRemainingExecutionDeadline(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `UPDATE jobs SET attempt_count=1,execution_started_at_ms=?,execution_deadline_at_ms=? WHERE id='job'`, now-3590000, now+10000)
	processed := false
	processor := recoveryProcess(func(ctx context.Context, _ metadatascrape.WorkerClaim, _ string) (int, string, error) {
		processed = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 11*time.Second {
			t.Errorf("reclaimed execution granted a new hour: deadline=%v", deadline)
		}
		return 0, "", nil
	})
	if err := metadatascrape.NewWorker(NewWorker(database), processor, recoveryNow).Run(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}
	if !processed {
		t.Fatal("remaining execution did not run")
	}
}

func TestMetadataExpiredLeaseResumesPersistedExecution(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='old-worker',
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=? WHERE id='job'`, now-100000, now+10000, now-1, now-60001)
	processed := false
	processor := recoveryProcess(func(context.Context, metadatascrape.WorkerClaim, string) (int, string, error) {
		processed = true
		return 0, "", nil
	})
	clock := recoveryTime
	worker := metadatascrape.NewWorker(NewWorker(database), processor, func() time.Time { return clock })
	if err := worker.Run(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}
	var state string
	var available, deadline int64
	if err := database.QueryRowContext(t.Context(), `SELECT state,available_at_ms,execution_deadline_at_ms FROM jobs WHERE id='job'`).Scan(&state, &available, &deadline); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || available != now+1000 || deadline != now+10000 || processed {
		t.Fatalf("recovery did not queue original execution: %s/%d/%d/%v", state, available, deadline, processed)
	}
	clock = clock.Add(time.Second)
	if err := worker.Run(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}
	if !processed {
		t.Fatal("expired owned execution remained stranded without processing")
	}
}

type resumeLookup struct{ calls int }

func (lookup *resumeLookup) Lookup(context.Context, hasheous.ContentHashes, bool) (metadatascrape.ResolvedLookup, error) {
	lookup.calls++
	return metadatascrape.ResolvedLookup{Result: hasheous.LookupResult{Outcome: hasheous.OutcomeMiss, RequestDigest: strings.Repeat("d", 64)}}, nil
}

func TestMetadataResumeSkipsTerminalEvidenceWithoutDuplicatingAttempt(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `INSERT INTO content_hash_evidence(id,scrape_run_id,profile,crc32,query_order,payload_released_at_ms,created_at_ms)
 VALUES('evidence','run','RAW_FILE','12345678',0,0,?)`, now)
	recoveryExec(t, database, `INSERT INTO metadata_provider_responses(id,provider,request_digest,outcome,raw_payload_state,
 fetched_at_ms,expires_at_ms) VALUES('response','HASHEOUS',?,'MISS','NONE',?,?)`, strings.Repeat("c", 64), now, now+1000)
	recoveryExec(t, database, `INSERT INTO metadata_scrape_query_attempts(id,scrape_run_id,content_hash_evidence_id,
 provider_response_id,attempt_no,source,created_at_ms) VALUES('attempt','run','evidence','response',1,'NETWORK',?)`, now)
	lookup := &resumeLookup{}
	repository := NewWorker(database)
	recorder := metadatascrape.NewRecorder(NewRecorder(database), nil, recoveryNow)
	processor := metadatascrape.NewProcessor(repository, lookup, recorder)
	if err := metadatascrape.NewWorker(repository, processor, recoveryNow).Run(t.Context(), "run"); err != nil {
		t.Fatalf("terminal evidence was replayed: %v", err)
	}
	if lookup.calls != 0 {
		t.Fatalf("terminal evidence queried %d times", lookup.calls)
	}
}

type resumeCachedLookup struct{}

func (resumeCachedLookup) Lookup(context.Context, hasheous.ContentHashes, bool) (metadatascrape.ResolvedLookup, error) {
	return metadatascrape.ResolvedLookup{CachedResponseID: "cached", Result: hasheous.LookupResult{Outcome: hasheous.OutcomeMiss}}, nil
}

func TestMetadataResumedCacheHitKeepsNextAttemptNumber(t *testing.T) {
	database := recoveryDatabase(t)
	now := recoveryTime.UnixMilli()
	recoveryExec(t, database, `INSERT INTO content_hash_evidence(id,scrape_run_id,profile,crc32,query_order,payload_released_at_ms,created_at_ms)
 VALUES('evidence','run','RAW_FILE','12345678',0,0,?)`, now)
	recoveryExec(t, database, `INSERT INTO metadata_provider_responses(id,provider,request_digest,outcome,raw_payload_state,
 fetched_at_ms,expires_at_ms) VALUES('response','HASHEOUS',?,'TIMEOUT','NONE',?,?)`, strings.Repeat("c", 64), now, now+1000)
	recoveryExec(t, database, `INSERT INTO metadata_provider_responses(id,provider,request_digest,outcome,raw_payload_state,
 fetched_at_ms,expires_at_ms) VALUES('cached','HASHEOUS',?,'MISS','NONE',?,?)`, strings.Repeat("d", 64), now, now+1000)
	recoveryExec(t, database, `INSERT INTO metadata_scrape_query_attempts(id,scrape_run_id,content_hash_evidence_id,
 provider_response_id,attempt_no,source,created_at_ms) VALUES('attempt','run','evidence','response',1,'NETWORK',?)`, now)
	repository := NewWorker(database)
	processor := metadatascrape.NewProcessor(repository, resumeCachedLookup{}, metadatascrape.NewRecorder(NewRecorder(database), nil, recoveryNow))
	if err := metadatascrape.NewWorker(repository, processor, recoveryNow).Run(t.Context(), "run"); err != nil {
		t.Fatal(err)
	}
	var count, maximum int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*),max(attempt_no) FROM metadata_scrape_query_attempts WHERE content_hash_evidence_id='evidence'`).Scan(&count, &maximum); err != nil {
		t.Fatal(err)
	}
	if count != 2 || maximum != 2 {
		t.Fatalf("resumed attempts=%d/%d", count, maximum)
	}
}
