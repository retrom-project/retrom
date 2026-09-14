package libraryimport

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	composition "retrom/internal/bootstrap/composition/libraryimport"

	librarypersistence "retrom/internal/repo/libraryimport"
	tagpersistence "retrom/internal/repo/tagging"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/scummvm"
	"retrom/internal/capability/security/authn"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

type Service struct {
	scummVMDetector        *scummvm.Detector
	database               *sql.DB
	blobs                  *blobstore.Store
	now                    func() time.Time
	scraper                *metadatascrape.Service
	tags                   *tagging.Service
	multiDiscImportEnabled bool
	reviewDrafts           *libraryservice.ReviewDrafts
	workerMu               sync.Mutex
	worker                 *composition.WorkerBundle
	workerClosed           bool
}

func (service *Service) WithMultiDiscImportEnabled(enabled bool) *Service {
	service.multiDiscImportEnabled = enabled
	return service
}

func reviewActor(ctx context.Context) authn.Actor {
	return authn.ActorFromContext(ctx, "release-setup")
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func New(database *sql.DB, now func() time.Time, scraper ...*metadatascrape.Service) *Service {
	service := &Service{
		database: database, now: now, tags: tagging.New(tagpersistence.New(database), now),
	}
	service.reviewDrafts = composition.NewReviewDrafts(
		database, now,
		service.ensureCompatibleDraftValidation,
		service.selectScummVMCandidate,
	)
	if len(scraper) > 0 {
		service.scraper = scraper[0]
	}
	return service
}

// Reconfigure reuses the unresolved rejected files from an existing import. The
// cloned UploadSession owns new logical UploadFiles but points at the same CAS
// blobs, so the browser never has to upload the bytes again.
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
	_ = librarypersistence.NewReconfigurations(service.database).RemoveUnused(ctx, uploadID)
}
