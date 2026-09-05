package runtimeprovider

import (
	"database/sql"
	"errors"
	"testing"
)

func TestCheckpointGuardProtectsOnlyDurableSaves(t *testing.T) {
	for _, test := range []struct {
		name, provider, target, format string
		deleted                        any
		blocked                        bool
	}{
		{"durable unreadable", "fixture", "target", "state-v1", nil, true},
		{"durable readable", "fixture", "target", "state-v2", nil, false},
		{"deleted save", "fixture", "target", "state-v1", 1, false},
		{"another target", "fixture", "other", "state-v1", nil, false},
		{"another provider", "other", "target", "state-v1", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			// No review tables exist here: ephemeral trial state is not an upgrade guard.
			_, err = database.ExecContext(t.Context(), `
CREATE TABLE save_states(game_id TEXT,checkpoint_format TEXT,deleted_at_ms INTEGER);
CREATE TABLE game_variants(game_id TEXT,provider_id TEXT,target_id TEXT);
`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(t.Context(), `INSERT INTO game_variants VALUES('game',?,?)`, test.provider, test.target); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(t.Context(), `INSERT INTO save_states VALUES('game',?,?)`, test.format, test.deleted); err != nil {
				t.Fatal(err)
			}
			transaction, err := database.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = transaction.Rollback() }()
			upgrade := projectionFixture("1.1.0", "b", []string{"state-v2"})
			err = validateCheckpointFormats(t.Context(), transaction, "fixture", upgrade.providers[0].targets[0])
			if errors.Is(err, ErrProviderCheckpointUnreadable) != test.blocked {
				t.Fatalf("blocked=%v error=%v", test.blocked, err)
			}
			if err != nil && !errors.Is(err, ErrProviderCheckpointUnreadable) {
				t.Fatal(err)
			}
		})
	}
}
