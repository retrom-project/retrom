package platforminstance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type boundaryRepository struct {
	Repository
	scope  WriteScope
	writes int
}

func (repository *boundaryRepository) WithWrite(_ context.Context, work func(WriteScope) error) error {
	repository.writes++
	return work(repository.scope)
}

type unavailableCore struct{ Reader }

func (unavailableCore) CoreEnabled(context.Context, string, string) (bool, error) { return false, nil }

func TestInvalidCreateNeverOpensTransaction(t *testing.T) {
	t.Parallel()
	repository := &boundaryRepository{}
	service := New(repository, time.Now)
	for _, input := range []CreateInput{{Name: ""}, {Name: " leading"}, {Name: "Valid", Description: "\x00"}} {
		if _, err := service.Create(t.Context(), AuditActor{}, input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid input: %v", err)
		}
	}
	if repository.writes != 0 {
		t.Fatalf("invalid input opened %d transactions", repository.writes)
	}
}

func TestUnavailableCoreCannotCreateDirectory(t *testing.T) {
	t.Parallel()
	repository := &boundaryRepository{scope: WriteScope{Reader: unavailableCore{}}}
	service := New(repository, time.Now)
	if _, err := service.Create(t.Context(), AuditActor{}, CreateInput{Name: "Library", PlatformID: "gba", DefaultCoreID: "disabled"}); !errors.Is(err, ErrDefaultCoreInvalid) {
		t.Fatalf("unavailable core: %v", err)
	}
	if repository.writes != 1 {
		t.Fatalf("transaction count = %d", repository.writes)
	}
}

func TestSlugSelectionKeepsReservedNames(t *testing.T) {
	t.Parallel()
	slug, err := NextSlug(SlugBase("My Library", "gba"), []string{"my-library", "my-library-2", "my-library-4"})
	if err != nil {
		t.Fatal(err)
	}
	if slug != "my-library-3" {
		t.Fatalf("slug = %q", slug)
	}
}
