package importworkflow

import (
	"time"

	"retrom/internal/cleanup"
	commands "retrom/internal/composition/libraryimport"
	"retrom/internal/core/scummvm"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

type Inputs struct {
	Database         dbapi.DB
	Files            *filestore.Store
	Tags             *tagging.Service
	Scraper          *metadatascrape.Service
	ScummVMDetector  *scummvm.Detector
	Now              func() time.Time
	MultiDiscEnabled bool
}

// New constructs the import workflow without starting workers or opening transactions.
func New(input Inputs) (*libraryimport.Service, libraryimport.Dependencies) {
	if input.Database == nil || input.Files == nil || input.Tags == nil {
		panic("importworkflow: database, files and tags are required")
	}
	if input.Now == nil {
		input.Now = time.Now
	}
	deps := assemble(input)
	return libraryimport.New(deps, libraryimport.Options{Now: input.Now, MultiDiscEnabled: input.MultiDiscEnabled}), deps
}

func assemble(input Inputs) libraryimport.Dependencies {
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
	worker := libraryservice.NewImportWorker(libraryservice.ImportWorkerDependencies{
		Queue: executions, Control: executions, Recovery: executions, Preparation: preparation, Creations: creations,
	}, libraryservice.ImportWorkerSettings{
		Now: now, Report: func(err error) { cleanup.Error("ordinary import worker", err) },
		RecoverPublications: approvals.Recover,
	})
	admissions := commands.NewImportAdmissions(database, worker, tags, libraryservice.ImportAdmissionOptions{
		Now: now, MultiDiscEnabled: input.MultiDiscEnabled, MetadataScraperAvailable: input.Scraper != nil,
	})
	return libraryimport.Dependencies{
		Database: database, Files: files, Tags: tags, Preparation: preparation, Creations: creations,
		Approvals: approvals, Discards: discards, Executions: executions, Worker: worker, Admissions: admissions,
		Reconfigurations:    commands.NewReconfigurations(database, now, files, creations),
		Drafts:              commands.NewReviewDrafts(database, tags, now),
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
