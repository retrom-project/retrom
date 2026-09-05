package runtimeprovider

import (
	"database/sql"
	"errors"
	"testing"
)

func TestCheckpointGuardProtectsOnlyDurableSavesAndLiveReviewPayloads(t *testing.T) {
	for _, test := range []struct {
		name, state string
		expires     int64
		blocked     bool
	}{
		{"pending restore", "CHECKPOINTED", 200, true},
		{"restore running", "RESTORED", 200, true},
		{"passed", "PASSED", 200, false},
		{"failed", "FAILED", 200, false},
		{"expired state", "EXPIRED", 200, false},
		{"elapsed lifetime", "CHECKPOINTED", 100, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			_, err = database.ExecContext(t.Context(), `
CREATE TABLE save_states(game_id TEXT,checkpoint_format TEXT,deleted_at_ms INTEGER);
CREATE TABLE game_variants(game_id TEXT,provider_id TEXT,target_id TEXT);
CREATE TABLE rpgmaker_runtime_validations(id TEXT,provider_id TEXT,target_id TEXT,state TEXT,expires_at_ms INTEGER);
CREATE TABLE rpgmaker_runtime_validation_checkpoints(validation_id TEXT,checkpoint_format TEXT);
INSERT INTO rpgmaker_runtime_validation_checkpoints VALUES('review','state-v1');
`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(t.Context(), `INSERT INTO rpgmaker_runtime_validations VALUES('review','fixture','target',?,?)`, test.state, test.expires); err != nil {
				t.Fatal(err)
			}
			transaction, err := database.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = transaction.Rollback() }()
			upgrade := projectionFixture("1.1.0", "b", []string{"state-v2"})
			err = validateCheckpointFormats(t.Context(), transaction, "fixture", upgrade.providers[0].targets[0], 100)
			if errors.Is(err, ErrProviderCheckpointUnreadable) != test.blocked {
				t.Fatalf("blocked=%v error=%v", test.blocked, err)
			}
			if err != nil && !errors.Is(err, ErrProviderCheckpointUnreadable) {
				t.Fatal(err)
			}
		})
	}
}
