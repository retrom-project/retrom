package launch

import (
	"errors"
	"testing"

	"retrom/internal/database/postgres"
	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

func TestPlayActivityIsAtomicAndCountsOnlyAcceptedDuration(t *testing.T) {
	db, err := postgres.Open(testpostgres.DSN(t), postgres.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), `CREATE TABLE play_sessions(
id TEXT PRIMARY KEY,launch_session_id TEXT UNIQUE,profile_id TEXT,game_id TEXT,
started_at_ms BIGINT,last_reported_at_ms BIGINT,active_duration_ms BIGINT,
state TEXT,version BIGINT,created_at_ms BIGINT,updated_at_ms BIGINT);
CREATE TABLE profile_game_activity(profile_id TEXT,game_id TEXT,last_played_at_ms BIGINT,
active_duration_ms BIGINT CHECK(active_duration_ms>=0),session_count BIGINT,
PRIMARY KEY(profile_id,game_id));`)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPlay(db)
	plan := application.PlaySnapshotPlan{
		PlayID: "play", NowMS: 1000, ActiveDurationMS: 100,
		Source: application.PlaySource{LaunchID: "launch", ProfileID: "profile", GameID: "game"},
	}
	cause := errors.New("abort after projection")
	err = repository.WithPlay(t.Context(), func(scope application.PlayScope) error {
		if err := scope.Write.Snapshot(t.Context(), plan); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("rollback cause: %v", err)
	}
	for _, table := range []string{"play_sessions", "profile_game_activity"} {
		var count int
		if err := dbapi.QueryRowContext(t.Context(), db, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial write to %s: %d %v", table, count, err)
		}
	}
	write := func() error {
		return repository.WithPlay(t.Context(), func(scope application.PlayScope) error { return scope.Write.Snapshot(t.Context(), plan) })
	}
	if err := write(); err != nil {
		t.Fatal(err)
	}
	plan.Current = &application.PlayRecord{ID: "play", Version: 1, ActiveDurationMS: 100, State: "ACTIVE"}
	plan.ActiveDurationMS = 200
	if err := write(); err != nil {
		t.Fatal(err)
	}
	if err := write(); !errors.Is(err, application.ErrBlocked) {
		t.Fatalf("stale version accepted: %v", err)
	}
	plan.Current.Version, plan.Current.ActiveDurationMS = 2, 200
	if err := write(); err != nil {
		t.Fatal(err)
	}
	var duration, count int64
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT active_duration_ms,session_count
FROM profile_game_activity WHERE profile_id='profile' AND game_id='game'`).Scan(&duration, &count); err != nil || duration != 200 || count != 1 {
		t.Fatalf("repeated sample counted twice: duration=%d count=%d err=%v", duration, count, err)
	}
}
