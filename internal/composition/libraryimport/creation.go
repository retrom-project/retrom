// Package libraryimport assembles import preparation and creation dependencies.
package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/core/scummvm"
	"retrom/internal/filestore"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

type CreationOptions struct {
	Blobs            *filestore.Store
	Tags             *tagging.Service
	Scraper          *metadatascrape.Service
	ScummVMDetector  *scummvm.Detector
	MultiDiscEnabled bool
}

func NewCreations(database dbapi.DB, now func() time.Time,
	preparation *application.ImportPreparation, options CreationOptions,
) *application.ImportCreations {
	var scraper application.ImportMetadata
	if options.Scraper != nil {
		scraper = options.Scraper
	}
	return application.NewImportCreations(
		repository.NewImportCreations(database), preparation, options.Tags, scraper,
		application.ImportCreationSettings{Now: now, MultiDiscEnabled: options.MultiDiscEnabled},
	)
}

func NewPreparation(database dbapi.DB, options CreationOptions) *application.ImportPreparation {
	return application.NewImportPreparation(
		repository.BindImportFacts(database), repository.BindPreparationCatalog(database),
		options.Blobs, application.ImportPreparationOptions{
			MultiDiscEnabled: options.MultiDiscEnabled, MetadataScraperAvailable: options.Scraper != nil,
			ScummVMDetector: options.ScummVMDetector,
		},
	)
}
