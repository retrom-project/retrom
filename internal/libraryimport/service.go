package libraryimport

import (
	"context"
	"fmt"
	"sync"
	"time"

	dbapi "retrom/internal/database"

	composition "retrom/internal/composition/libraryimport"

	librarypersistence "retrom/internal/persistence/libraryimport"
	tagpersistence "retrom/internal/persistence/tagging"

	"retrom/internal/authn"
	"retrom/internal/core/scummvm"
	"retrom/internal/filestore"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

type Service struct {
	scummVMDetector        *scummvm.Detector
	database               dbapi.DB
	blobs                  *filestore.Store
	now                    func() time.Time
	scraper                *metadatascrape.Service
	tags                   *tagging.Service
	multiDiscImportEnabled bool
	reviewDrafts           *libraryservice.ReviewDrafts
	workerMu               sync.Mutex
	worker                 *composition.WorkerBundle
	workerClosed           bool
	attachmentCancels      map[uint64]context.CancelFunc
	nextAttachmentID       uint64
	attachments            sync.WaitGroup
}

func (service *Service) WithMultiDiscImportEnabled(enabled bool) *Service {
	service.multiDiscImportEnabled = enabled
	return service
}

func reviewActor(ctx context.Context) authn.Actor {
	return authn.ActorFromContext(ctx, "release-setup")
}

func (service *Service) WithFileStore(blobs *filestore.Store) *Service {
	service.blobs = blobs
	return service
}

func New(database dbapi.DB, now func() time.Time, scraper ...*metadatascrape.Service) *Service {
	service := &Service{
		database: database, now: now, tags: tagging.New(tagpersistence.New(database), now),
	}
	service.reviewDrafts = composition.NewReviewDrafts(
		database, service.tags, now,
		service.ensureCompatibleDraftValidation,
		service.selectScummVMCandidate,
	)
	if len(scraper) > 0 {
		service.scraper = scraper[0]
	}
	return service
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
	result, err := composition.NewReconfigurations(
		service.database, service.now, service.creationDependencies(),
	).Reconfigure(ctx, libraryservice.ReconfigurationRequest{
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
