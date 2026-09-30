package libraryimport

import (
	"context"
	"fmt"
	"sync"
	"time"

	dbapi "retrom/internal/database"

	librarypersistence "retrom/internal/persistence/libraryimport"

	"retrom/internal/authn"
	"retrom/internal/filestore"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/tagging"
)

type MultiDiscAttachmentCreator interface {
	Create(context.Context, string, int64, MultiDiscAttachmentRequest) (MultiDiscAttachmentCreated, error)
}

type Dependencies struct {
	AttachmentCreator   MultiDiscAttachmentCreator
	Database            dbapi.DB
	Files               *filestore.Store
	Tags                *tagging.Service
	Preparation         *libraryservice.ImportPreparation
	Creations           *libraryservice.ImportCreations
	Reconfigurations    *libraryservice.Reconfigurations
	Approvals           *libraryservice.ReviewApprovals
	Drafts              *libraryservice.ReviewDrafts
	Discards            *libraryservice.ReviewDiscards
	BatchDiscards       *libraryservice.ReviewBatchDiscards
	Retries             *libraryservice.ImportItemRetries
	Cancellations       *libraryservice.ImportBatchCancellations
	Deduplicator        *libraryservice.ReviewDeduplicator
	AttachmentSources   *libraryservice.MultiDiscAttachmentSources
	AttachmentCommits   *libraryservice.MultiDiscAttachmentCommits
	AttachmentTerminals *libraryservice.MultiDiscAttachmentTerminals
	Executions          *libraryservice.ImportExecutions
	Worker              *libraryservice.ImportWorker
	Admissions          *libraryservice.ImportAdmissions
}

type Options struct {
	Now              func() time.Time
	MultiDiscEnabled bool
}

type Service struct {
	attachmentCreator      MultiDiscAttachmentCreator
	database               dbapi.DB
	blobs                  *filestore.Store
	now                    func() time.Time
	tags                   *tagging.Service
	multiDiscImportEnabled bool
	preparation            *libraryservice.ImportPreparation
	creations              *libraryservice.ImportCreations
	reconfigurations       *libraryservice.Reconfigurations
	approvals              *libraryservice.ReviewApprovals
	reviewDrafts           *libraryservice.ReviewDrafts
	discards               *libraryservice.ReviewDiscards
	batchDiscards          *libraryservice.ReviewBatchDiscards
	retries                *libraryservice.ImportItemRetries
	cancellations          *libraryservice.ImportBatchCancellations
	deduplicator           *libraryservice.ReviewDeduplicator
	attachmentSources      *libraryservice.MultiDiscAttachmentSources
	attachmentCommits      *libraryservice.MultiDiscAttachmentCommits
	attachmentTerminals    *libraryservice.MultiDiscAttachmentTerminals
	executions             *libraryservice.ImportExecutions
	worker                 *libraryservice.ImportWorker
	admissions             *libraryservice.ImportAdmissions
	workerMu               sync.Mutex
	workerClosed           bool
	attachmentCancels      map[uint64]context.CancelFunc
	nextAttachmentID       uint64
	attachments            sync.WaitGroup
}

func New(deps Dependencies, options Options) *Service {
	deps.validate()
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{
		attachmentCreator: deps.AttachmentCreator,
		database:          deps.Database, blobs: deps.Files, tags: deps.Tags, now: options.Now,
		multiDiscImportEnabled: options.MultiDiscEnabled,
		preparation:            deps.Preparation, creations: deps.Creations, reconfigurations: deps.Reconfigurations,
		approvals: deps.Approvals, reviewDrafts: deps.Drafts, discards: deps.Discards,
		batchDiscards: deps.BatchDiscards, retries: deps.Retries, cancellations: deps.Cancellations,
		deduplicator:      deps.Deduplicator,
		attachmentSources: deps.AttachmentSources, attachmentCommits: deps.AttachmentCommits,
		attachmentTerminals: deps.AttachmentTerminals, executions: deps.Executions, worker: deps.Worker,
		admissions: deps.Admissions,
	}
}

func (deps Dependencies) validate() {
	for _, missing := range []bool{
		deps.AttachmentCreator == nil,
		deps.Database == nil, deps.Files == nil, deps.Tags == nil, deps.Preparation == nil,
		deps.Creations == nil, deps.Reconfigurations == nil, deps.Approvals == nil, deps.Drafts == nil,
		deps.Deduplicator == nil, deps.AttachmentSources == nil,
		deps.Discards == nil, deps.BatchDiscards == nil, deps.Retries == nil, deps.Cancellations == nil,
		deps.AttachmentCommits == nil, deps.AttachmentTerminals == nil, deps.Executions == nil,
		deps.Worker == nil, deps.Admissions == nil,
	} {
		if missing {
			panic("libraryimport: all workflow dependencies are required")
		}
	}
}

func reviewActor(ctx context.Context) authn.Actor {
	return authn.ActorFromContext(ctx, "release-setup")
}

// Reconfigure reuses the unresolved rejected files from an existing import. The
// replacement UploadSession receives independent copies, so the browser never
// has to upload the bytes again and either session can release its own files.
func (service *Service) Reconfigure(
	ctx context.Context,
	sourceImportJobID string,
	expectedVersion int64,
	request ReconfigureRequest,
) (Created, error) {
	result, err := service.reconfigurations.Reconfigure(ctx, libraryservice.ReconfigurationRequest{
		SourceImportJobID:      sourceImportJobID,
		ExpectedVersion:        expectedVersion,
		TargetPlatformInstance: request.TargetPlatformInstanceID,
		MetadataProvider:       request.MetadataProvider,
		TagIDs:                 request.TagIDs,
	})
	if err != nil {
		return Created{}, fmt.Errorf("reconfigure library import: %w", err)
	}
	return result.Created, nil
}

func (service *Service) removeUnusedClonedUpload(ctx context.Context, uploadID string) {
	_ = librarypersistence.NewReconfigurations(service.database).RemoveUnused(ctx, uploadID, service.now().UnixMilli())
}
