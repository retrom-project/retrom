package payloadrelease

import (
	"context"
	"database/sql"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	sources "retrom/internal/payloadrelease"
	repository "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/payloadrelease"
)

func New(ctx context.Context, database *sql.DB, blobs *blobstore.Store, now func() time.Time, retention time.Duration) (*application.Service, error) {
	files := sources.NewSources(blobs)
	return application.New(ctx, application.Dependencies{
		Lifecycle: repository.NewLifecycle(database), Worker: repository.NewWorker(database),
		GC: repository.NewGC(database), Garbage: repository.NewGarbage(database),
		Effects: repository.NewReleaseEffects(database), Expiration: repository.NewExpiration(database),
		Retirement: repository.NewRetirement(database), Impact: repository.NewImpactQueries(database),
		Files: files, Waiter: files,
	}, application.Options{Now: now, Retention: retention, Report: func(err error) { cleanup.Error("payload worker", err) }})
}
