//go:build integration

package persistence_test

import (
	"reflect"
	"testing"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestOriginalMetadataMigrationPreservesExistingGameAndFiles(t *testing.T) {
	f := testsupport.Library(t)
	f.Publish(t)
	before, err := f.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the released schema shape in this test's private database. The
	// existing physical records have no original attachment to backfill.
	if err = f.Repository.Execute(t.Context(), "ALTER TABLE game_file_tab DROP COLUMN ext_info"); err != nil {
		t.Fatal(err)
	}
	if err = f.Repository.Execute(t.Context(), "DELETE FROM schema_migrations_tab WHERE id='002_game_file_ext_info'"); err != nil {
		t.Fatal(err)
	}
	if err = f.Repository.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := f.Repository.ContentSnapshot(t.Context(), f.Game.ID)
	if err != nil || !reflect.DeepEqual(before, after) || after.Files[0].ExtInfo != (model.GameFileTabExtInfo{}) {
		t.Fatalf("migration changed an existing game: %+v err=%v", after, err)
	}
	count, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_tab WHERE ext_info='{}'::jsonb")
	if err != nil || count != int64(len(after.Files)) {
		t.Fatalf("original metadata default rows=%d err=%v", count, err)
	}
}
