package application

import (
	"sync"
	"sync/atomic"

	gamevariant "retrom/internal/service/gamevariant"

	dbapi "retrom/internal/database"

	cleanupcomposition "retrom/internal/composition/cleanupjobs"

	firmwareservice "retrom/internal/service/firmware"

	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/service/accounts"
	biosservice "retrom/internal/service/bios"
	catalogservice "retrom/internal/service/catalog"
	diagnosticsservice "retrom/internal/service/diagnostics"
	"retrom/internal/service/favorites"
	gameassetsservice "retrom/internal/service/gameassets"
	"retrom/internal/service/gamecontent"
	gamelistservice "retrom/internal/service/gamelist"
	gamemetadataservice "retrom/internal/service/gamemetadata"
	homeservice "retrom/internal/service/home"
	idempotencyservice "retrom/internal/service/idempotency"
	"retrom/internal/service/immersive"
	"retrom/internal/service/importdiscard"
	"retrom/internal/service/isolation"
	"retrom/internal/service/jobs"
	launchservice "retrom/internal/service/launch"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/service/mediaaccess"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/platforminstance"
	readinessservice "retrom/internal/service/readiness"
	"retrom/internal/service/saves"
	"retrom/internal/service/serverimport"
	"retrom/internal/service/sourceimport"
	"retrom/internal/service/tagging"
	"retrom/internal/service/uploads"
)

// Services owns the process services shared by transports and background work.
type Services struct {
	lifecycleMu         sync.Mutex
	started             bool
	stopping            atomic.Bool
	shutdown            *shutdownGroup
	catalogs            *catalogTask
	Database            dbapi.DB
	ReadinessDatabase   dbapi.DB
	ReadinessService    *readinessservice.Service
	Dependencies        *dependencies.Set
	Blobs               *filestore.Store
	Credentials         *retromruntime.Credentials
	Uploads             *uploads.Service
	Importer            *libraryimport.Service
	ImportDiscards      *importdiscard.Service
	Launcher            *launchservice.Service
	Variants            *gamevariant.Service
	ReviewScreenshots   *libraryservice.ScreenshotSaver
	LaunchSources       *launch.Sources
	JobService          *jobs.Service
	Immersive           *immersive.Service
	Firmware            *firmwareservice.Service
	BiosService         *biosservice.Service
	CatalogService      *catalogservice.Service
	MediaAccess         *mediaaccess.Service
	Metadata            *metadatascrape.Service
	GameContent         *gamecontent.Service
	GameImpact          *gamecontent.ImpactQueries
	GameListService     *gamelistservice.Service
	HomeService         *homeservice.Service
	GameAssets          *gameassetsservice.Service
	GameMetadata        *gamemetadataservice.Service
	SaveService         *saves.Service
	RpgIsolation        *isolation.Service
	FavoriteService     *favorites.Service
	TagService          *tagging.Service
	ReviewQueue         *libraryservice.ReviewQueue
	ReviewDetails       *libraryservice.ReviewDetails
	ReviewCoverUploads  *libraryservice.ReviewCoverUploads
	ReviewDiscards      *libraryservice.ReviewDiscards
	ReviewApprovals     *libraryservice.ReviewApprovals
	ReviewBulkApprovals *libraryservice.ReviewBulk
	ImportAdmissions    *libraryservice.ImportAdmissions
	MetadataEvidence    *metadatascrape.EvidenceQueries
	ServerImports       *serverimport.Service
	SourceImports       *sourceimport.Service
	CleanupJobs         *cleanupcomposition.Service
	PlatformDirectories *platforminstance.Service
	Accounts            *accounts.Service
	DiagnosticsService  *diagnosticsservice.Service
	IdempotencyService  *idempotencyservice.Service
}
