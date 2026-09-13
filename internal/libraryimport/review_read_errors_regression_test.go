//go:build integration

package libraryimport

import (
	"database/sql"
	"errors"
	"testing"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"

	_ "modernc.org/sqlite"
)

func TestReviewReadHelpersPreserveDatabaseFailure(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	cause := database.QueryRowContext(t.Context(), "SELECT 1").Err()
	if cause == nil {
		t.Fatal("closed database did not fail")
	}
	for _, read := range []struct {
		name string
		run  func() error
	}{
		{"content identity", func() error { _, err := importItemContentIdentity(t.Context(), database, "item"); return err }},
		{"duplicate matches", func() error { _, err := findDuplicateGames(t.Context(), database, "item", "gba"); return err }},
		{"arcade relations", func() error {
			_, _, err := application.LoadArcadeClosure(t.Context(), repository.BindArcadeRelations(database), "dat", "machine")
			return err
		}},
	} {
		t.Run(read.name, func(t *testing.T) {
			if err := read.run(); !errors.Is(err, cause) {
				t.Fatalf("database cause lost: %v, want %v", err, cause)
			}
		})
	}
}
