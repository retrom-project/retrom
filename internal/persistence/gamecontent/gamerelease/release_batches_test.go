package gamerelease_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/testsupport"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/cleanupjobs"
	"retrom/internal/persistence/gamecontent/gamerelease"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/cleanupjobs"
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
	if _, err := recordstore.InsertRows(t.Context(), db, "game_files", `WITH RECURSIVE pages(n) AS (
 SELECT 1 UNION ALL SELECT n+1 FROM pages WHERE n<401)
 INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order)
 SELECT '018fbe68-0000-7000-8000-000000000001','COMPANION','file-'||n,?,n FROM pages`, testsupport.FileMetadata("paged").Record); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT payload_release_job_id FROM games WHERE id='018fbe68-0000-7000-8000-000000000001'`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	work, found, err := cleanupjobs.BindWorker(db).Read.Current(t.Context(), jobID)
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
	// Completed replay neither removes a new reference nor adds another DeletionQueue job.
	if err := service.Execute(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	assertReleasePage(t, db, 0, "RELEASED", 1)
}

func assertReleasePage(t *testing.T, db dbapi.DB, want int64, state string, candidates int64) {
	t.Helper()
	var rows, deletion int64
	var payload string
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT
 (SELECT count(*) FROM game_files WHERE game_id='018fbe68-0000-7000-8000-000000000001'),
 (SELECT payload_state FROM games WHERE id='018fbe68-0000-7000-8000-000000000001'),
 (SELECT count(*) FROM jobs WHERE kind='PATH_DELETE')`).Scan(&rows, &payload, &deletion); err != nil {
		t.Fatal(err)
	}
	if rows != want || payload != state || deletion != candidates {
		t.Fatalf("rows=%d payload=%s DeletionQueue=%d; want %d/%s/%d", rows, payload, deletion, want, state, candidates)
	}
}
