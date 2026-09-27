package application

import (
	"context"
	"fmt"
	"time"

	variantcomposition "retrom/internal/composition/gamevariant"

	dbapi "retrom/internal/database"

	gamecontentpersistence "retrom/internal/persistence/gamecontent"

	"retrom/internal/composition"
	payloadcomposition "retrom/internal/composition/cleanupjobs"
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
	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/hasheous"
	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	favoritepersistence "retrom/internal/persistence/favorites"
	idempotencypersistence "retrom/internal/persistence/idempotency"
	mediapersistence "retrom/internal/persistence/mediaaccess"
	platformpersistence "retrom/internal/persistence/platforminstance"
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
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/mediaaccess"
	"retrom/internal/service/platforminstance"
	"retrom/internal/service/saves"
	"retrom/internal/service/tagging"
	"retrom/internal/service/uploads"
)

type Inputs struct {
	Config                      config.Config
	Database, ReadinessDatabase dbapi.DB
	Dependencies                *dependencies.Set
	Files                       *filestore.Store
	Credentials                 *retromruntime.Credentials
	Accounts                    *accounts.Service
	Now                         func() time.Time
	ScummVMDetector             *scummvm.Detector
	RuntimeProvider             *runtimelaunch.Builder
}

// New assembles services without starting background work.
func New(input Inputs) (*Services, error) {
	config, database, dependencySet := input.Config, input.Database, input.Dependencies
	blobs, credentials, accountService, now := input.Files, input.Credentials, input.Accounts, input.Now
	if now == nil {
		now = time.Now
	}
	payloadReleaseService, err := payloadcomposition.New(context.Background(), database, blobs, now)
	if err != nil {
		return nil, fmt.Errorf("initialize cleanup jobs: %w", err)
	}
	scraper := composition.NewMetadata(database, blobs, hasheous.New(nil, nil, now), now)

	launchSources := launch.NewSources(blobs, credentials).
		WithRPGRuntimeOriginTemplate(config.RPGRuntimeOriginTemplate).WithRuntimeProvider(input.RuntimeProvider)
	variants := variantcomposition.New(database, launchSources, now)

	launcher := launchcomposition.New(database, launchSources, config.PublicOrigin.String(), now, variants.Dispatch)
	importer := libraryimport.New(database, now, scraper).
		WithFileStore(blobs).
		WithMultiDiscImportEnabled(config.MultiDiscImportEnabled)
	if input.ScummVMDetector != nil {
		importer.WithScummVMDetector(input.ScummVMDetector)
	}

	firmwareService := firmwareservice.New(firmwarepersistence.New(database), now).WithFileStore(blobs).
		WithCleanup(payloadReleaseService)
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

	tagService := tagging.New(tagpersistence.New(database), now)
	server := &Services{
		Database:          database,
		ReadinessDatabase: database,
		ReadinessService:  composition.NewReadiness(database),
		Dependencies:      dependencySet,
		Blobs:             blobs,
		Credentials:       credentials,

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
		CleanupJobs:         payloadReleaseService,
		DiagnosticsService:  composition.NewDiagnostics(database),
		PlatformDirectories: platforminstance.New(platformpersistence.New(database), now),
		Metadata:            scraper,
		GameContent: gamecontent.New(gamecontentpersistence.New(database), now).WithFileStore(blobs).
			WithCleanup(payloadReleaseService).
			WithMultiDiscImportEnabled(config.MultiDiscImportEnabled),
		GameImpact:      gamecontent.NewImpactQueries(gamecontentpersistence.NewImpactQueries(database)),
		GameListService: composition.NewGameList(database),
		HomeService:     composition.NewHome(database, tagService),
		GameAssets:      composition.NewGameAssets(database, blobs, now, payloadReleaseService),
		GameMetadata:    composition.NewGameMetadata(database, blobs, payloadReleaseService, now),
		SaveService:     saves.New(savepersistence.New(database), blobs, now),
		RpgIsolation:    isolation.New(isolationpersistence.New(database), config.RPGRuntimeOriginTemplate, now),
		FavoriteService: favorites.New(favoritepersistence.New(database), now),
		TagService:      tagService,

		IdempotencyService: idempotencyservice.New(idempotencypersistence.New(database)),
	}
	server.ReviewQueue = composition.NewLibraryReviewQueue(database, server.TagService)
	server.ReviewDetails = composition.NewLibraryReviewDetails(database)
	server.ReviewScreenshots = composition.NewLibraryReviewScreenshots(database, blobs, now)
	server.ReviewCoverUploads = composition.NewLibraryReviewCoverUploads(database, blobs, now)
	server.ReviewDiscards = composition.NewLibraryReviewDiscards(database, now)
	server.ReviewApprovals = composition.NewLibraryReviewApprovals(database, now, blobs)
	server.ReviewBulkApprovals = librarycomposition.NewReviewBulk(database, server.ReviewApprovals, now)

	server.ImportAdmissions = composition.NewLibraryImportAdmissions(
		database, importer, libraryservice.ImportAdmissionOptions{
			Now: now, MultiDiscEnabled: config.MultiDiscImportEnabled, MetadataScraperAvailable: true,
		},
	)
	server.JobService = composition.WithSourceJobCancellation(server.JobService, sourceImportService)
	server.JobService = librarycomposition.WithJobCancellation(server.JobService, database, now)
	server.MediaAccess = mediaaccess.New(mediapersistence.New(database))
	server.MetadataEvidence = composition.NewMetadataEvidenceQueries(database)
	server.ImportDiscards = composition.NewImportDiscard(
		database,
		libraryimport.NewDiscardWorkflow(importer),
		sourceImportService,
		now,
	)

	if input.ReadinessDatabase != nil {
		server.ReadinessDatabase = input.ReadinessDatabase
		server.ReadinessService = composition.NewReadiness(input.ReadinessDatabase)
	}
	return server, nil
}
