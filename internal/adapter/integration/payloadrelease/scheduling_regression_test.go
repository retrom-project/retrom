package payloadrelease

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/testkit/testsupport"
)

func schedulingGame(t *testing.T) *sql.DB {
	t.Helper()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(10)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO games(id,platform_instance_id,title,title_initial,
description,developer,publisher,genre,metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,
source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms)
VALUES('schedule-game',(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
'Fixture','F','','','','','ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','scheduling-fixture','[]',?,
'PUBLISHED','fixture',1,1,1)`, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return database.SQL
}

type failedSchedulingCount struct {
	driver.Result
	cause error
}

func (result failedSchedulingCount) RowsAffected() (int64, error) { return 0, result.cause }

func TestGameReleaseSchedulingPreservesAffectedRowFailure(t *testing.T) {
	t.Parallel()
	db := schedulingGame(t)
	cause := errors.New("release owner affected-row failure")
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "UPDATE games SET status='DELETED'") {
				for _, arg := range args {
					if arg.Value == "schedule-game" {
						hits.Add(1)
						return failedSchedulingCount{Result: result, cause: cause}, nil
					}
				}
			}
			return result, nil
		},
	})
	tx, err := faultDB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	id, err := ScheduleGameDeletion(t.Context(), tx, "schedule-game", 1, 10)
	if !errors.Is(err, cause) || id != "" || hits.Load() != 1 {
		t.Fatalf("scheduling lost storage failure: job=%q error=%v hits=%d", id, err, hits.Load())
	}
	var jobs int
	var state string
	err = tx.QueryRowContext(t.Context(), `SELECT status,(SELECT count(*) FROM jobs WHERE scope_id='schedule-game')
FROM games WHERE id='schedule-game'`).Scan(&state, &jobs)
	if err != nil || state != "DELETED" || jobs != 1 {
		t.Fatalf("fault preceded actual atomic writes: state=%s jobs=%d err=%v", state, jobs, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	err = db.QueryRowContext(t.Context(), `SELECT status,(SELECT count(*) FROM jobs WHERE scope_id='schedule-game')
FROM games WHERE id='schedule-game'`).Scan(&state, &jobs)
	if err != nil || state != "PUBLISHED" || jobs != 0 {
		t.Fatalf("schedule failure retained writes: state=%s jobs=%d err=%v", state, jobs, err)
	}
}
