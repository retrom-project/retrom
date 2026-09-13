package emulationstationimport

import (
	"context"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/repo/dberrors"
	application "retrom/internal/service/emulationstationimport"
)

type fixtureDatabaseDiagnostics struct{}

func (fixtureDatabaseDiagnostics) DatabaseCause(err error) string { return dberrors.Classify(err) }
func (service *Service) sources() *Sources {
	return &Sources{
		roots:       service.roots,
		blobs:       service.blobs,
		guard:       application.NewSourceGuard(service.executionControl()),
		diagnostics: fixtureDatabaseDiagnostics{},
	}
}

func (service *Service) sanitizeTechnicalDetail(err error) string {
	return service.sources().Sanitize(err)
}

func (service *Service) copySource(
	ctx context.Context,
	root Root,
	unit work,
	selectedPath, relativePath string,
	size int64,
	facts string,
) (blobstore.Metadata, error) {
	return service.sources().copySource(ctx, root, unit, selectedPath, relativePath, size, facts)
}
