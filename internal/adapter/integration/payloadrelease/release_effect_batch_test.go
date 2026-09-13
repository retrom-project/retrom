package payloadrelease

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/repo/dbexec"
	"retrom/internal/testkit/testsupport"
)

func TestReleaseGameRemovesEveryFileAcrossBatchBoundary(t *testing.T) {
	fixture := queuedReleaseWorker(t)
	seedEffectGamePayload(t, fixture.database)
	seedEffectFileBatch(t, fixture)
	claim := claimEffect(t, fixture)
	var deletes, rows atomic.Int64
	service := effectFaultService(t, fixture, testsupport.SQLFaultHooks{AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
		if strings.HasPrefix(strings.TrimSpace(query), "DELETE FROM game_files") && effectBound(args, "schedule-game") {
			changed, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			deletes.Add(1)
			rows.Add(changed)
		}
		return result, nil
	}})
	if err := service.execute(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	if deletes.Load() != 2 || rows.Load() != 201 {
		t.Fatalf("batches=%d rows=%d", deletes.Load(), rows.Load())
	}
	assertEffectGameGraphReleased(t, fixture)
	var remaining int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM game_files WHERE game_id='schedule-game'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("remaining files=%d error=%v", remaining, err)
	}
}

func seedEffectFileBatch(t *testing.T, fixture releaseWorkerFixture) {
	t.Helper()
	tx, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	for index := range 201 {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO game_files(game_id,role,logical_name,blob_id,sort_order)
VALUES('schedule-game','COMPANION',?,'effect-blob',?)`, fmt.Sprintf("effect-companion-%03d", index), index)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
