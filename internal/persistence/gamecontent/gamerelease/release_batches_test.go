package gamerelease_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/gamecontent/gamerelease"
	"retrom/internal/persistence/payloadworker"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/payloadrelease"
)

type batchAuthority struct{ checks, failAt int }

func (authority *batchAuthority) CheckInScope(context.Context, application.WorkerScope, application.Work) error {
	authority.checks++
	if authority.checks == authority.failAt {
		return application.ErrExecutionLost
	}
	return nil
}

func TestReleaseCommitsBoundedPagesAndResumesAfterLostAuthority(t *testing.T) {
	db := effectRepositoryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
 VALUES('paged-blob',?,1,?,?,?,'application/octet-stream',0)`, strings.Repeat("a", 64), strings.Repeat("a", 32), strings.Repeat("a", 40), strings.Repeat("a", 8)); err != nil {
		t.Fatal(err)
	}
	if _, err := recordstore.CreateReferences(t.Context(), db, "game_files", `WITH RECURSIVE pages(n) AS (
 SELECT 1 UNION ALL SELECT n+1 FROM pages WHERE n<401)
 INSERT INTO game_files(game_id,role,logical_name,blob_id,sort_order)
 SELECT 'effect-game','COMPANION','file-'||n,'paged-blob',n FROM pages`); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT payload_release_job_id FROM games WHERE id='effect-game'`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	work, found, err := payloadworker.BindWorker(db).Read.Current(t.Context(), jobID)
	if err != nil || !found {
		t.Fatalf("work found=%v err=%v", found, err)
	}
	input, err := application.DecodeWork(work)
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.UnixMilli(10) }
	authority := &batchAuthority{failAt: 4}
	service := application.NewReleaseEffects(gamerelease.NewEffects(db), authority, nil, now)
	unit := application.Execution{Work: work, Input: input}
	if err := service.Execute(t.Context(), unit); !errors.Is(err, application.ErrExecutionLost) {
		t.Fatalf("lost authority=%v", err)
	}
	assertReleasePage(t, db, 201, "RELEASING", 0)
	authority.failAt = 0
	if err := service.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	assertReleasePage(t, db, 0, "RELEASED", 1)
	// Completed replay neither removes a new reference nor adds another GC job.
	if err := service.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	assertReleasePage(t, db, 0, "RELEASED", 1)
}

func assertReleasePage(t *testing.T, db dbapi.DB, want int64, state string, candidates int64) {
	t.Helper()
	var rows, refs, gc int64
	var payload string
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT
 (SELECT count(*) FROM game_files WHERE game_id='effect-game'),
 (SELECT ref_count FROM blobs WHERE id='paged-blob'),
 (SELECT payload_state FROM games WHERE id='effect-game'),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id='paged-blob')`).Scan(&rows, &refs, &payload, &gc); err != nil {
		t.Fatal(err)
	}
	if rows != want || refs != want || payload != state || gc != candidates {
		t.Fatalf("rows=%d refs=%d payload=%s GC=%d; want %d/%s/%d", rows, refs, payload, gc, want, state, candidates)
	}
}
