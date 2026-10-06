package store

import (
	"errors"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/testsupport/testpostgres"
)

func TestEmulatorNumbersMigrationStartsAboveExistingAndBoundsNumbers(t *testing.T) {
	for _, existing := range []int64{0, 9000} {
		t.Run(time.UnixMilli(existing).Format("150405"), func(t *testing.T) {
			db := openMigrationTestDatabase(t, testpostgres.DSN(t))
			defer func() { _ = db.Close() }()
			if _, err := db.ExecContext(t.Context(), `CREATE TABLE game_variants(emulator_game_id BIGINT)`); err != nil {
				t.Fatal(err)
			}
			if existing > 0 {
				if _, err := db.ExecContext(t.Context(), `INSERT INTO game_variants VALUES(?)`, existing); err != nil {
					t.Fatal(err)
				}
			}
			sources, err := migrationSources()
			if err != nil {
				t.Fatal(err)
			}
			if err := runMigration(t.Context(), db, sources[1], time.Now); err != nil {
				t.Fatal(err)
			}
			id, err := recordstore.NextEmulatorGameID(t.Context(), db)
			if err != nil || id != max(existing, 1000)+1 {
				t.Fatalf("initial number=%d err=%v", id, err)
			}
			assertEmulatorNumberExhaustion(t, db)
			var count int
			if err := dbapi.QueryRowContext(t.Context(), db, `SELECT count(*) FROM schema_migrations WHERE version=2`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("migration record=%d err=%v", count, err)
			}
		})
	}
}

func assertEmulatorNumberExhaustion(t *testing.T, db dbapi.DB) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `SELECT setval('emulator_game_numbers',9007199254740990,true)`); err != nil {
		t.Fatal(err)
	}
	id, err := recordstore.NextEmulatorGameID(t.Context(), db)
	if err != nil || id != 9007199254740991 {
		t.Fatalf("last number=%d err=%v", id, err)
	}
	_, err = recordstore.NextEmulatorGameID(t.Context(), db)
	var state interface{ SQLState() string }
	if !errors.As(err, &state) || state.SQLState() != "2200H" {
		t.Fatalf("sequence wrapped or masked exhaustion: %v", err)
	}
}
