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

func TestServerSourcePreservesBlobReadFailure(t *testing.T) {
	fixture, request := ownedSourceFixture(t)
	if err := fixture.database.Close(); err != nil {
		t.Fatal(err)
	}
	var ignored int64
	expected := fixture.database.QueryRowContext(fixture.ctx, `SELECT size_bytes FROM blobs WHERE id=?`, request.Files[0].BlobID).Scan(&ignored)
	_, _, _, err := fixture.service.validateServerFiles(fixture.ctx, request.Files)
	if expected == nil || !errors.Is(err, expected) {
		t.Fatalf("blob read cause lost: got=%v expected=%v", err, expected)
	}
}

type metadataEntropyFailure struct{}

func (metadataEntropyFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
