package payloadrelease

import (
	"testing"
	"time"
)

func TestGCRestoredReferenceCanRestartRetentionAtTheSameClock(t *testing.T) {
	t.Parallel()
	fixture := newGCSchedulingFixture(t)
	before := stageGCTestCandidate(t, fixture)
	_, err := fixture.database.ExecContext(t.Context(), `INSERT INTO game_assets
 (id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
 VALUES('gc-returned-reference','schedule-game','manual-gc-blob','COVER',0,1,1,'image/png',10)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.stageAllUnreferenced(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `DELETE FROM game_assets WHERE id='gc-returned-reference'`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.stageAllUnreferenced(t.Context()); err != nil {
		t.Fatalf("new candidate collided with retired job at same clock: %v", err)
	}
	var id string
	var available, count int64
	err = fixture.database.QueryRowContext(t.Context(), `SELECT job.id,job.available_at_ms,
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob')
 FROM jobs job JOIN blob_gc_candidates candidate ON candidate.gc_job_id=job.id WHERE candidate.blob_id='manual-gc-blob'`).
		Scan(&id, &available, &count)
	if err != nil || id == before || available != 10+(24*time.Hour).Milliseconds() || count != 1 {
		t.Fatalf("invalid renewed retention: id=%s old=%s available=%d count=%d error=%v", id, before, available, count, err)
	}
	if err := fixture.service.stageAllUnreferenced(t.Context()); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE kind='BLOB_GC'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 2 {
		t.Fatalf("repeated reconciliation duplicated active candidate: %d jobs", jobs)
	}
}
