package libraryimport

import (
	"context"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReconfigurations(
	database dbapi.DB,
	now func() time.Time,
	options CreationOptions,
) *application.Reconfigurations {
	creations := NewCreations(database, now, options)
	var copyFile func(context.Context, string) (filestore.Metadata, error)
	if options.Blobs != nil {
		copyFile = options.Blobs.Copy
	}
	return application.NewReconfigurations(
		repository.NewReconfigurations(database),
		func(ctx context.Context, request application.ImportRequest,
			creationOptions application.ImportCreationOptions,
		) (application.ImportCreationResult, error) {
			return creations.Create(ctx, request, creationOptions)
		}, copyFile, now,
	)
}
