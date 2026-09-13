package libraryimport

import (
	"context"
	"database/sql"
	"time"

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
		func(ctx context.Context, request application.ImportRequest,
			creationOptions application.ImportCreationOptions,
		) (application.ImportCreationResult, error) {
			return creations.Create(ctx, request, creationOptions)
		}, now,
	)
}
