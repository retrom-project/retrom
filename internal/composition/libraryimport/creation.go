// Package libraryimport assembles import preparation and creation dependencies.
package libraryimport

import (
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/core/scummvm"
	"retrom/internal/filestore"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
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
	preparation *libraryservice.ImportPreparation, options CreationOptions,
) *libraryservice.ImportCreations {
	var scraper libraryservice.ImportMetadata
	if options.Scraper != nil {
		scraper = options.Scraper
	}
	return libraryservice.NewImportCreations(
		repository.NewImportCreations(database), preparation, options.Tags, scraper,
		libraryservice.ImportCreationSettings{Now: now, MultiDiscEnabled: options.MultiDiscEnabled},
	)
}

func NewPreparation(database dbapi.DB, options CreationOptions) *libraryservice.ImportPreparation {
	return libraryservice.NewImportPreparation(
		repository.BindImportFacts(database), repository.BindPreparationCatalog(database),
		options.Blobs, libraryservice.ImportPreparationOptions{
			MultiDiscEnabled: options.MultiDiscEnabled, MetadataScraperAvailable: options.Scraper != nil,
			ScummVMDetector: options.ScummVMDetector,
		},
	)
}
