package payloadrelease

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/repo/dbexec"
	"retrom/internal/testkit/testsupport"
)

func queuedConsumptionEffect(t *testing.T) releaseWorkerFixture {
	t.Helper()
	database := schedulingGame(t)
	_, err := database.ExecContext(t.Context(), `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('effect-upload','COMPLETE','FILES',1,0,?,1,10000,10,10);
INSERT INTO upload_consumptions(id,upload_session_id,consumer_type,consumer_id,version,created_at_ms)
VALUES('effect-consumption','effect-upload','GAME_ASSET','effect-asset',1,10)`, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	id, err := ScheduleConsumption(t.Context(), tx, "effect-consumption", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	clock := &atomic.Int64{}
	clock.Store(10)
	service, err := New(database, nil, func() time.Time { return time.UnixMilli(clock.Load()) }, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return releaseWorkerFixture{database: database, service: service, jobID: id, now: clock}
}

func claimEffect(t *testing.T, fixture releaseWorkerFixture) claimedJob {
	t.Helper()
	claim, found, err := fixture.service.claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim=%#v found=%t error=%v", claim, found, err)
	}
	return claim
}

func effectFaultService(t *testing.T, fixture releaseWorkerFixture, hooks testsupport.SQLFaultHooks) *Service {
	t.Helper()
	pool := testsupport.OpenSQLFaultDatabase(t, fixture.database, hooks)
	service, err := New(pool, nil, fixture.service.now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service
}

func effectBound(args []driver.NamedValue, id string) bool {
	for _, arg := range args {
		if arg.Value == id {
			return true
		}
	}
	return false
}

func assertEffectRetained(t *testing.T, fixture releaseWorkerFixture, scope ScopeType) {
	t.Helper()
	if scope == ScopeGame {
		var state string
		if err := fixture.database.QueryRowContext(t.Context(), `SELECT payload_state FROM games WHERE id='schedule-game'`).Scan(

			&state,
		); err != nil || state != "RELEASING" {
			t.Fatalf("game partial release=%s %v", state, err)
		}
		return
	}
	var released sql.NullInt64
	var version int64
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT released_at_ms,version FROM upload_consumptions WHERE id='effect-consumption'`).Scan(

		&released,

		&version,
	); err != nil || released.Valid || version != 1 {
		t.Fatalf("consumption partial release=%#v version=%d %v", released, version, err)
	}
}

func gameRemainingFault(cause error, hits *atomic.Int64) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
		q := strings.Join(strings.Fields(query), " ")
		if strings.HasPrefix(
			q,
			"SELECT (SELECT count(*) FROM game_assets WHERE game_id=?)+",
		) && effectBound(
			args,
			"schedule-game",
		) {
			hits.Add(1)
			return cause
		}
		return nil
	}}
}
