package platforminstance_test

import (
	"context"
	"errors"
	"testing"

	platformpersistence "retrom/internal/repo/platforminstance"
	"retrom/internal/service/platforminstance"
)

func TestCanceledCommitRollsBackAndReleasesConnection(t *testing.T) {
	t.Parallel()
	_, database := newService(t)
	repository := platformpersistence.New(database)
	directory := platforminstance.NewDirectory{
		ID: "01980000-0000-7000-8000-000000009902", Slug: "canceled-library",
		Input:       platforminstance.CreateInput{PlatformID: "gba", DefaultCoreID: "mgba", Name: "Canceled Library"},
		CreatedAtMS: 1_786_000_000_000,
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := repository.WithWrite(ctx, func(scope platforminstance.WriteScope) error {
		if err := scope.Directories.Insert(ctx, directory); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled commit: %v", err)
	}
	var count int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM platform_instances WHERE id=?`, directory.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canceled transaction retained %d directories", count)
	}
	if err := repository.WithWrite(t.Context(), func(scope platforminstance.WriteScope) error {
		return scope.Directories.Insert(t.Context(), directory)
	}); err != nil {
		t.Fatalf("connection was not reusable: %v", err)
	}
}
