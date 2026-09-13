// Package libraryimport assembles import preparation and creation dependencies.
package libraryimport

import (
	"database/sql"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/scummvm"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

type CreationOptions struct {
	Blobs            *blobstore.Store
	Tags             *tagging.Service
	Scraper          *metadatascrape.Service
	ScummVMDetector  *scummvm.Detector
	MultiDiscEnabled bool
}

func NewCreations(database *sql.DB, now func() time.Time, options CreationOptions) *application.ImportCreations {
	return application.NewImportCreations(
		repository.NewImportCreations(database), NewPreparation(database, options), options.Tags, options.Scraper,
		application.ImportCreationSettings{Now: now, MultiDiscEnabled: options.MultiDiscEnabled},
	)
}

func NewPreparation(database *sql.DB, options CreationOptions) *application.ImportPreparation {
	return application.NewImportPreparation(
		repository.BindImportFacts(database), repository.BindPreparationCatalog(database),
		options.Blobs, application.ImportPreparationOptions{
			MultiDiscEnabled: options.MultiDiscEnabled, MetadataScraperAvailable: options.Scraper != nil,
			ScummVMDetector: options.ScummVMDetector,
		},
	)
}
