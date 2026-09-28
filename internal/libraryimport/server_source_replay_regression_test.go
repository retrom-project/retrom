//go:build integration

package libraryimport

import (
	"errors"
	"reflect"
	"testing"

	libraryservice "retrom/internal/service/libraryimport"
)

func TestOwnedSourceReplayRejectsIdentityErrorsWithoutContentClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		paths []string
	}{
		{name: "missing request paths"},
		{name: "different request paths", paths: []string{"games/other.gba"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, request := ownedSourceFixture(t)
			created, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			request.Intent.PrimaryPaths = test.paths
			result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			if !errors.Is(err, ErrInvalid) || errors.Is(err, libraryservice.ErrSourceGrouping) ||
				!reflect.DeepEqual(result, ServerImportResult{}) || ownedImportCount(t, fixture) != 1 {
				t.Fatalf("replay identity classified as content: %#v %v", result, err)
			}
			assertOwnedSourceBinding(t, fixture, created)
		})
	}
}

func TestOwnedSourceReplayRejectsMissingPersistentPathsWithoutContentClassification(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	fixture.execute(t, `DELETE FROM source_import_item_files WHERE item_id='018fbe68-0000-7000-8000-000000000021'`)
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if !errors.Is(err, ErrVersionConflict) || errors.Is(err, libraryservice.ErrSourceGrouping) ||
		!reflect.DeepEqual(result, ServerImportResult{}) || ownedImportCount(t, fixture) != 1 {
		t.Fatalf("missing persistent identity classified as content: %#v %v", result, err)
	}
	assertOwnedSourceBinding(t, fixture, created)
}
