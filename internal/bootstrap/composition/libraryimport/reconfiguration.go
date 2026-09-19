package libraryimport

import (
	"context"
	"database/sql"
	"time"

	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func NewReconfigurations(
	database *sql.DB,
	now func() time.Time,
	options CreationOptions,
) *application.Reconfigurations {
	creations := NewCreations(database, now, options)
	return application.NewReconfigurations(
		repository.NewReconfigurations(database),
		func(ctx context.Context, request libraryimportmodel.ImportRequest,
			creationOptions libraryimportmodel.ImportCreationOptions,
		) (libraryimportmodel.ImportCreationResult, error) {
			return creations.Create(ctx, request, creationOptions)
		}, now,
	)
}
