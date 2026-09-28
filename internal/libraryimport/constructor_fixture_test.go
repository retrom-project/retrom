package libraryimport

import (
	"testing"
	"time"

	"retrom/internal/cleanup"
	commands "retrom/internal/composition/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"

	"retrom/internal/core/scummvm"
	dbapi "retrom/internal/database"
	dbsqlite "retrom/internal/database/sqlite"
	"retrom/internal/filestore"

	tagpersistence "retrom/internal/persistence/tagging"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

type testImportOptions struct {
	Database         dbapi.DB
	Files            *filestore.Store
	Tags             *tagging.Service
	Now              func() time.Time
	MultiDiscEnabled bool
	Scraper          *metadatascrape.Service
	ScummVMDetector  *scummvm.Detector
}

func newTestImporter(t *testing.T, database dbapi.DB, files *filestore.Store, options testImportOptions) *Service {
	t.Helper()
	if database == nil {
		db, err := dbsqlite.Open(":memory:", dbsqlite.Options{})
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
	options.Database, options.Files, options.Tags = database, files, tags
	service := New(assembleTestDependencies(options), Options{Now: options.Now, MultiDiscEnabled: options.MultiDiscEnabled})
	t.Cleanup(service.Close)
	return service
}

func assembleTestDependencies(input testImportOptions) Dependencies {
	database, files, tags, now := input.Database, input.Files, input.Tags, input.Now
	options := commands.CreationOptions{
		Blobs: files, Tags: tags, Scraper: input.Scraper,
		ScummVMDetector: input.ScummVMDetector, MultiDiscEnabled: input.MultiDiscEnabled,
	}
	preparation := commands.NewPreparation(database, options)
	creations := commands.NewCreations(database, now, preparation, options)
	approvals := commands.NewReviewApprovals(database, now, files, tags)
	discards := commands.NewReviewDiscards(database, now)
	executions := commands.NewExecutions(database, now)
	validator := NewDraftValidator(now)
	worker := libraryservice.NewImportWorker(libraryservice.ImportWorkerDependencies{
		Queue: executions, Control: executions, Recovery: executions, Preparation: preparation, Creations: creations,
	}, libraryservice.ImportWorkerSettings{
		Now: now, Report: func(err error) { cleanup.Error("ordinary import worker", err) },
		RecoverPublications: approvals.Recover,
	})
	admissions := commands.NewImportAdmissions(database, worker, tags, libraryservice.ImportAdmissionOptions{
		Now: now, MultiDiscEnabled: input.MultiDiscEnabled, MetadataScraperAvailable: input.Scraper != nil,
	})
	return Dependencies{
		Database: database, Files: files, Tags: tags, Preparation: preparation, Creations: creations,
		Approvals: approvals, Discards: discards, Executions: executions, Worker: worker, Admissions: admissions,
		Reconfigurations:    commands.NewReconfigurations(database, now, files, creations),
		Drafts:              commands.NewReviewDrafts(database, tags, now, validator.Refresh, validator.SelectScummVM),
		PreviewValidations:  commands.NewReviewPreviewValidations(database, now, validator.Refresh),
		BatchDiscards:       commands.NewReviewBatchDiscards(database, discards, now),
		Retries:             commands.NewImportItemRetries(database, now),
		Cancellations:       commands.NewImportBatchCancellations(database, now),
		Deduplicator:        commands.NewReviewDeduplicator(database, now),
		AttachmentCreator:   commands.NewMultiDiscAttachments(database, now),
		AttachmentSources:   commands.NewMultiDiscAttachmentSources(database),
		AttachmentCommits:   commands.NewMultiDiscAttachmentCommits(database, now),
		AttachmentTerminals: commands.NewMultiDiscAttachmentTerminals(database, now),
	}
}
