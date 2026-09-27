package libraryimport

import (
	"context"
	"errors"
	"io"
	"testing"

	"retrom/internal/core/rpgmaker/detector"
	"retrom/internal/filestore"
)

func TestPreparedArtifactsPreserveStorageFailureAndClearResult(t *testing.T) {
	cause := errors.New("artifact storage unavailable")
	service := NewImportArtifacts(failingPreparedArtifactBlobs{cause: cause})
	groups := []PreparedGroup{{
		RPGProfile: &detector.Profile{ExpectedGeneration: detector.RPGXP},
		Sources:    []PreparedSource{{File: ImportFile{SHA256: "digest", Size: 1}, LogicalName: "game.dat"}},
	}}
	result, err := service.Prepare(context.Background(), groups, nil)
	if !errors.Is(err, cause) || result != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(groups[0].ValidationFiles) != 0 {
		t.Fatal("failed preparation changed caller's groups")
	}
}

type failingPreparedArtifactBlobs struct{ cause error }

func (blobs failingPreparedArtifactBlobs) OpenID(string) (io.ReadCloser, error) {
	return nil, blobs.cause
}

func (blobs failingPreparedArtifactBlobs) Put(io.Reader) (filestore.Metadata, error) {
	return filestore.Metadata{}, blobs.cause
}
