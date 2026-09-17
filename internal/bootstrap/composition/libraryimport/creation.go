// Package libraryimport assembles import preparation and creation dependencies.
package libraryimport

import (
	"database/sql"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/scummvm"
	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
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

func NewCreations(database *sql.DB, now func() time.Time, options CreationOptions) *libraryimportservice.ImportCreations {
	return libraryimportservice.NewImportCreations(
		repository.NewImportCreations(database), NewPreparation(database, options), options.Tags, options.Scraper,
		libraryimportmodel.ImportCreationSettings{Now: now, MultiDiscEnabled: options.MultiDiscEnabled},
	)
}

func NewPreparation(database *sql.DB, options CreationOptions) *libraryimportservice.ImportPreparation {
	return libraryimportservice.NewImportPreparation(
		repository.BindImportFacts(database), repository.BindPreparationCatalog(database),
		options.Blobs, libraryimportmodel.ImportPreparationOptions{
			MultiDiscEnabled: options.MultiDiscEnabled, MetadataScraperAvailable: options.Scraper != nil,
			ScummVMDetector: options.ScummVMDetector,
		},
	)
}
