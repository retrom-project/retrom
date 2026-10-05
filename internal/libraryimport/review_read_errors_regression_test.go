//go:build integration

package libraryimport

import (
	"errors"
	"testing"

	"retrom/internal/content/arcade"
	arcaderecords "retrom/internal/persistence/arcade"

	dbapi "retrom/internal/database"
	dbpostgres "retrom/internal/database/postgres"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport/testpostgres"
)

func TestReviewReadHelpersPreserveDatabaseFailure(t *testing.T) {
	t.Parallel()
	database, err := dbpostgres.Open(testpostgres.DSN(t), dbpostgres.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	cause := errors.Unwrap(dbapi.QueryRowContext(t.Context(), database, "SELECT 1").Err())
	if cause == nil {
		t.Fatal("closed database did not fail")
	}
	for _, read := range []struct {
		name string
		run  func() error
	}{
		{"content identity", func() error {
			_, err := libraryservice.NewContentDuplicates(repository.BindContentDuplicates(database)).Identity(t.Context(), "item")
			return err
		}},
		{"duplicate matches", func() error {
			_, err := libraryservice.NewContentDuplicates(repository.BindContentDuplicates(database)).Matches(t.Context(), "item", "gba")
			return err
		}},
		{"arcade relations", func() error {
			_, _, err := arcade.LoadClosure(t.Context(), arcaderecords.New(database), "dat", "machine")
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
