package serverimport_test

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/runtime/runtime"
	"retrom/internal/bootstrap/composition"
	"retrom/internal/service/firmware"
	importservice "retrom/internal/service/serverimport"
)

type (
	Service            = importservice.Service
	Summary            = importservice.Summary
	CreateRequest      = importservice.CreateRequest
	work               = importservice.Work
	evaluatedCandidate = importservice.EvaluatedCandidate
	discoveredFile     = serversource.File
	walkCounts         = serversource.Counts
)

var (
	ErrQuery             = importservice.ErrQuery
	ErrNotCancellable    = importservice.ErrNotCancellable
	ErrNotRetryable      = importservice.ErrNotRetryable
	ErrCatalogInvalid    = importservice.ErrCatalogInvalid
	ErrActive            = importservice.ErrActive
	ErrPathInvalid       = importservice.ErrPathInvalid
	ValidateRelativePath = importservice.ValidateRelativePath
	rankCandidates       = importservice.RankCandidates
)

func New(database *sql.DB, blobs *blobstore.Store, installer *firmware.Service, credentials *runtime.Credentials, configured []serversource.Root, now func() time.Time) *Service {
	return composition.NewServerImports(database, blobs, installer, credentials, configured, now)
}

func openSelectedDirectory(path, relative string) (*os.File, error) {
	result, err := serversource.OpenSelectedDirectory(path, relative)
	if err != nil {
		return nil, fmt.Errorf("open test source directory: %w", err)
	}
	return result, nil
}

func walkFiles(directory *os.File, visit func(discoveredFile) error) (walkCounts, error) {
	result, err := importservice.WalkFilesForTest(directory, visit)
	if err != nil {
		return walkCounts{}, fmt.Errorf("walk test source: %w", err)
	}
	return result, nil
}
