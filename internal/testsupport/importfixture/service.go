package importfixture

import (
	"testing"
	"time"

	"retrom/internal/composition/importworkflow"
	"retrom/internal/core/scummvm"
	dbapi "retrom/internal/database"
	dbpostgres "retrom/internal/database/postgres"
	"retrom/internal/filestore"
	"retrom/internal/libraryimport"
	tagpersistence "retrom/internal/persistence/tagging"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

type Options struct {
	Now              func() time.Time
	MultiDiscEnabled bool
	Scraper          *metadatascrape.Service
	ScummVMDetector  *scummvm.Detector
}

func New(t *testing.T, database dbapi.DB, files *filestore.Store, options Options) *libraryimport.Service {
	t.Helper()
	if database == nil {
		db, err := dbpostgres.Open(":memory:", dbpostgres.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		database = db
	}
	if files == nil {
		var err error
		files, err = filestore.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	tags := tagging.New(tagpersistence.New(database), options.Now)
	service, _ := importworkflow.New(importworkflow.Inputs{
		Database: database, Files: files, Tags: tags, Now: options.Now, MultiDiscEnabled: options.MultiDiscEnabled,
		Scraper: options.Scraper, ScummVMDetector: options.ScummVMDetector,
	})
	t.Cleanup(service.Close)
	return service
}
