//go:build integration

package persistence_test

import (
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestRpgMakerSaveRetirementIsScopedAndRunsOnce(t *testing.T) {
	f := testsupport.Library(t)
	ctx := t.Context()
	const migration = "003_rpgmaker_native_saves"
	if err := f.Repository.Execute(ctx, "DELETE FROM schema_migrations_tab WHERE id=$1", migration); err != nil {
		t.Fatal(err)
	}
	write := func(core, userID string) string {
		t.Helper()
		id := uuid.NewString()
		if err := f.Repository.WriteSave(ctx, model.Save{
			ID: id, UserID: userID, Game: model.Game{ID: f.Game.ID}, Name: "progress", Kind: "game_save",
			StorageKey: "unused-" + id, PayloadHash: "hash-" + id, SizeBytes: 3, LastCommitID: id,
			Extinfo: model.Extinfo{CoreID: core},
		}, 1000, true); err != nil {
			t.Fatal(err)
		}
		return id
	}
	write("rpgmaker", f.Principal.User.ID)
	write("rpgmaker", uuid.NewString())
	other := write("other", f.Principal.User.ID)
	if err := f.Repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	retired, err := f.Repository.Count(ctx, `SELECT count(*) FROM save_tab
 WHERE extinfo->>'coreId'='rpgmaker' AND status='deleted' AND version=2 AND updated_at_ms>1000`)
	if err != nil || retired != 2 {
		t.Fatalf("retired=%d err=%v", retired, err)
	}
	current := write("rpgmaker", f.Principal.User.ID)
	if err = f.Repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{other, current} {
		active, readErr := f.Repository.Count(ctx, "SELECT count(*) FROM save_tab WHERE id=$1 AND status='active' AND version=1", id)
		if readErr != nil || active != 1 {
			t.Fatalf("save %s active=%d err=%v", id, active, readErr)
		}
	}
}
