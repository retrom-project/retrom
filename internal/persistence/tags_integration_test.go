//go:build integration

package persistence_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestTagNameConflictIsDistinctFromVersionConflict(t *testing.T) {
	t.Parallel()
	repository := testsupport.Database(t)
	ctx, userID := t.Context(), uuid.NewString()
	firstID, secondID := uuid.NewString(), uuid.NewString()
	for id, name := range map[string]string{firstID: "Action Games", secondID: "Adventure"} {
		if err := repository.WriteTag(ctx, id, userID, name, 0, 1000); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Action Games", "  ACTION   games  "} {
		err := repository.WriteTag(ctx, uuid.NewString(), userID, name, 0, 2000)
		assertTagConflict(t, err, model.ErrTagNameConflict)
	}
	err := repository.WriteTag(ctx, secondID, userID, "action games", 1, 2000)
	assertTagConflict(t, err, model.ErrTagNameConflict)
	unchanged, err := repository.Tag(ctx, secondID)
	if err != nil || unchanged.Name != "Adventure" || unchanged.Version != 1 {
		t.Fatalf("failed rename changed tag: %+v error=%v", unchanged, err)
	}
	if err = repository.WriteTag(ctx, secondID, userID, "Exploration", 1, 3000); err != nil {
		t.Fatal(err)
	}
	assertTagConflict(t, repository.WriteTag(ctx, secondID, userID, "Action Games", 1, 4000), model.ErrConflict)
	assertTagConflict(t, repository.WriteTag(ctx, secondID, userID, "Different", 0, 4000), model.ErrConflict)
	if err = repository.DeleteTag(ctx, firstID, userID, 1, 5000); err != nil {
		t.Fatal(err)
	}
	replacementID := uuid.NewString()
	if err = repository.WriteTag(ctx, replacementID, userID, "Action Games", 0, 6000); err != nil {
		t.Fatalf("deleted name was not reusable: %v", err)
	}
	replacement, err := repository.Tag(ctx, replacementID)
	if err != nil || replacement.Version != 1 || replacement.GameCount != 0 {
		t.Fatalf("new tag=%+v error=%v", replacement, err)
	}
}

func TestConcurrentTagNameCreatesHaveOneWinner(t *testing.T) {
	t.Parallel()
	repository := testsupport.Database(t)
	ctx, userID := t.Context(), uuid.NewString()
	start, results := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- repository.WriteTag(ctx, uuid.NewString(), userID, "Concurrent tag", 0, 1000)
		}()
	}
	close(start)
	winners, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			winners++
		case errors.Is(err, model.ErrTagNameConflict):
			conflicts++
		default:
			t.Fatalf("unexpected create failure: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d name conflicts=%d", winners, conflicts)
	}
}

func assertTagConflict(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want=%v", err, want)
	}
	if errors.Is(err, model.ErrConflict) == errors.Is(err, model.ErrTagNameConflict) {
		t.Fatalf("name and version conflicts were not distinct: %v", err)
	}
}
