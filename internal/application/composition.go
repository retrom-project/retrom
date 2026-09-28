package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	variantcomposition "retrom/internal/composition/gamevariant"

	dbapi "retrom/internal/database"

	gamecontentpersistence "retrom/internal/persistence/gamecontent"

	"retrom/internal/composition"
	cleanupcomposition "retrom/internal/composition/cleanupjobs"
	"retrom/internal/composition/importworkflow"
	librarycomposition "retrom/internal/composition/libraryimport"

	firmwarepersistence "retrom/internal/persistence/firmware"
	firmwareservice "retrom/internal/service/firmware"

	savepersistence "retrom/internal/persistence/saves"

	uploadpersistence "retrom/internal/persistence/uploads"

	isolationpersistence "retrom/internal/persistence/isolation"

	jobpersistence "retrom/internal/persistence/jobs"

	immersivepersistence "retrom/internal/persistence/immersive"

	tagpersistence "retrom/internal/persistence/tagging"

	launchcomposition "retrom/internal/composition/launch"
	"retrom/internal/config"
	"retrom/internal/core/scummvm"
	"retrom/internal/filestore"
	"retrom/internal/hasheous"
	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	favoritepersistence "retrom/internal/persistence/favorites"
	idempotencypersistence "retrom/internal/persistence/idempotency"
	mediapersistence "retrom/internal/persistence/mediaaccess"
	platformpersistence "retrom/internal/persistence/platforminstance"
	runtimesessionpersistence "retrom/internal/persistence/runtimesession"
	retromruntime "retrom/internal/runtime"
	runtimelaunch "retrom/internal/runtime/launch"
	"retrom/internal/serversource"
	"retrom/internal/service/accounts"
	"retrom/internal/service/favorites"
	"retrom/internal/service/gamecontent"
	idempotencyservice "retrom/internal/service/idempotency"
	"retrom/internal/service/immersive"
	"retrom/internal/service/isolation"
	"retrom/internal/service/jobs"
	"retrom/internal/service/mediaaccess"
	"retrom/internal/service/platforminstance"
	"retrom/internal/service/runtimesession"
	"retrom/internal/service/saves"
	"retrom/internal/service/tagging"
	"retrom/internal/service/uploads"

	"github.com/google/uuid"
)

type Inputs struct {
	Config                      config.Config
	IndexCatalogs               func(context.Context) error
	Database, ReadinessDatabase dbapi.DB
	Files                       *filestore.Store
	Credentials                 *retromruntime.Credentials
	Accounts                    *accounts.Service
	Now                         func() time.Time
	ScummVMDetector             *scummvm.Detector
	RuntimeProvider             *runtimelaunch.Builder
}

var ErrInvalidInputs = errors.New("application requires database, files, credentials and public origin")

// New assembles services without starting background work.
func New(ctx context.Context, input Inputs) (*Services, error) {
	if input.Database == nil || input.Files == nil || input.Credentials == nil || input.Config.PublicOrigin == nil {
		return nil, ErrInvalidInputs
	}
	config, database := input.Config, input.Database
	blobs, credentials, accountService, now := input.Files, input.Credentials, input.Accounts, input.Now
	if now == nil {
		now = time.Now
	}
	cleanupService, err := cleanupcomposition.New(ctx, database, blobs, now)
	if err != nil {
		return nil, fmt.Errorf("initialize cleanup jobs: %w", err)
	}
	scraper := composition.NewMetadata(database, blobs, hasheous.New(nil, nil, now), now)

	launchSources := launch.NewSources(blobs, credentials).
		WithRPGRuntimeOriginTemplate(config.RPGRuntimeOriginTemplate).WithRuntimeProvider(input.RuntimeProvider)
	variants := variantcomposition.New(database, launchSources, now)

	launcher := launchcomposition.New(database, launchSources, config.PublicOrigin.String(), now, variants.Dispatch)
	tagService := tagging.New(tagpersistence.New(database), now)
	importer, importDeps := importworkflow.New(importworkflow.Inputs{
		Database: database, Files: blobs, Tags: tagService, Now: now, Scraper: scraper,
		ScummVMDetector: input.ScummVMDetector, MultiDiscEnabled: config.MultiDiscImportEnabled,
	})

	firmwareService := firmwareservice.New(firmwareservice.Dependencies{
		Repository: firmwarepersistence.New(database), Files: blobs, Cleanup: cleanupService,
	}, now)
	serverImportService := composition.NewServerImports(
		database,
		blobs,
		firmwareService,
		credentials,
		serversource.FilesystemRoots(),
		now,
	)

	sourceImportService := composition.NewSourceImport(
		database, blobs, importer, credentials, serversource.FilesystemRoots(), now,
	)

	server := &Services{
		ImportReads:      librarycomposition.NewImportReads(database),
		GameMove:         composition.NewGameMove(database),
		ReadinessService: composition.NewReadiness(database),
		Blobs:            blobs,
		Credentials:      credentials,

		Accounts: accountService,

		Uploads:             uploads.New(uploadpersistence.New(database), blobs, config.DataDir, now),
		Importer:            importer,
		Launcher:            launcher,
		Variants:            variants,
		LaunchSources:       launchSources,
		JobService:          jobs.New(jobpersistence.New(database), now),
		Immersive:           immersive.New(immersivepersistence.New(database)),
		Firmware:            firmwareService,
		BiosService:         composition.NewBIOS(database),
		CatalogService:      composition.NewCatalog(database),
		ServerImports:       serverImportService,
		SourceImports:       sourceImportService,
		CleanupJobs:         cleanupService,
		DiagnosticsService:  composition.NewDiagnostics(database),
		PlatformDirectories: platforminstance.New(platformpersistence.New(database), now),
		Metadata:            scraper,
		GameContent: gamecontent.New(gamecontent.Dependencies{
			Repository: gamecontentpersistence.New(database), Files: blobs, Cleanup: cleanupService,
		}, gamecontent.Options{Now: now, MultiDiscEnabled: config.MultiDiscImportEnabled}),
		GameImpact:      gamecontent.NewImpactQueries(gamecontentpersistence.NewImpactQueries(database)),
		GameListService: composition.NewGameList(database),
		HomeService:     composition.NewHome(database, tagService),
		GameAssets:      composition.NewGameAssets(database, blobs, now, cleanupService),
		GameMetadata:    composition.NewGameMetadata(database, blobs, cleanupService, now),
		SaveService:     saves.New(savepersistence.New(database), blobs, now),
		RuntimeSessions: runtimesession.New(runtimesessionpersistence.New(database), runtimesession.Environment{
			Now: now, Sign: credentials.RuntimeSession,
			NewID: func() (string, error) { id, err := uuid.NewV7(); return id.String(), err },
		}),
		RpgIsolation:    isolation.New(isolationpersistence.New(database), config.RPGRuntimeOriginTemplate, now),
		FavoriteService: favorites.New(favoritepersistence.New(database), now),
		TagService:      tagService,

		IdempotencyService: idempotencyservice.New(idempotencypersistence.New(database)),
	}
	server.ReviewQueue = librarycomposition.NewReviewQueue(database, server.TagService)
	server.ReviewDetails = librarycomposition.NewReviewDetails(database)
	server.ReviewScreenshots = importworkflow.NewReviewScreenshots(database, blobs, now)
	server.ReviewCoverUploads = librarycomposition.NewReviewCoverUploads(database, blobs, now)
	server.ReviewDiscards = importDeps.Discards
	server.ReviewApprovals = importDeps.Approvals
	server.ReviewBulkApprovals = librarycomposition.NewReviewBulk(database, server.ReviewApprovals, now)

	server.ImportAdmissions = importDeps.Admissions
	server.JobService = composition.WithSourceJobCancellation(server.JobService, sourceImportService)
	server.JobService = librarycomposition.WithJobCancellation(server.JobService, importDeps.Executions)
	server.MediaAccess = mediaaccess.New(mediapersistence.New(database))
	server.MetadataEvidence = composition.NewMetadataEvidenceQueries(database)
	server.ImportDiscards = composition.NewImportDiscard(
		database,
		libraryimport.NewDiscardWorkflow(importer),
		sourceImportService,
		now,
	)

	if input.ReadinessDatabase != nil {
		server.ReadinessService = composition.NewReadiness(input.ReadinessDatabase)
	}
	server.catalogs = newCatalogTask(input.IndexCatalogs)
	server.shutdown = newShutdownGroup(server.workers(), server.CleanupJobs.Close)
	return server, nil
}
