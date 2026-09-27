//go:build integration

package libraryimport

import (
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
)

func TestOwnedSourcePreservesCreationEntropyFailure(t *testing.T) {
	fixture, request := ownedSourceFixture(t)
	uuid.SetRand(metadataEntropyFailure{})
	result, err := func() (ServerImportResult, error) {
		defer uuid.SetRand(nil)
		return fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	}()
	if !errors.Is(err, io.ErrUnexpectedEOF) || result.Created.ImportJobID != "" || result.Items != nil {
		t.Fatalf("source entropy failure ignored: %#v %v", result, err)
	}
	assertOwnedCreationRolledBack(t, fixture)
}

func TestServerSourceFactsDoNotDependOnGlobalCatalog(t *testing.T) {
	fixture, request := ownedSourceFixture(t)
	if err := fixture.database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := fixture.service.validateServerFiles(fixture.ctx, request.Files); err != nil {
		t.Fatal(err)
	}
}

type metadataEntropyFailure struct{}

func (metadataEntropyFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
