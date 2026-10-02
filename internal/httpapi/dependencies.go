package httpapi

import (
	"net/http"

	"retrom/internal/filestore"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/service/accounts"
	biosservice "retrom/internal/service/bios"
	catalogservice "retrom/internal/service/catalog"
	diagnosticsservice "retrom/internal/service/diagnostics"
	"retrom/internal/service/favorites"
	firmwareservice "retrom/internal/service/firmware"
	gameassetsservice "retrom/internal/service/gameassets"
	"retrom/internal/service/gamecontent"
	gamelistservice "retrom/internal/service/gamelist"
	gamemetadataservice "retrom/internal/service/gamemetadata"
	"retrom/internal/service/gamemove"
	"retrom/internal/service/gamevariant"
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
	"retrom/internal/service/runtimesession"
	"retrom/internal/service/saves"
	"retrom/internal/service/serverimport"
	"retrom/internal/service/sourceimport"
	"retrom/internal/service/tagging"
	"retrom/internal/service/uploads"
)

// Dependencies declares transport needs by feature. Only the process root assembles them.
type Dependencies struct {
	Account AccountDependencies
	Library LibraryDependencies
	Import  ImportDependencies
	Review  ReviewDependencies
	Play    PlayDependencies
	System  SystemDependencies
	Content ContentDependencies
}

// CleanupNotifier wakes cleanup without exposing its assembly or lifecycle.
type CleanupNotifier interface{ Signal() }

type AccountDependencies struct {
	Authenticator Authenticator
	Accounts      *accounts.Service
}

type LibraryDependencies struct {
	Catalog     *catalogservice.Service
	Directories *platforminstance.Service
	Firmware    *firmwareservice.Service
	BIOS        *biosservice.Service
	Content     *gamecontent.Service
	Impact      *gamecontent.ImpactQueries
	List        *gamelistservice.Service
	Home        *homeservice.Service
	Assets      *gameassetsservice.Service
	Metadata    *gamemetadataservice.Service
	Moves       *gamemove.Service
	Favorites   *favorites.Service
	Tags        *tagging.Service
	Immersive   *immersive.Service
}

type ImportDependencies struct {
	Uploads    *uploads.Service
	Importer   *libraryimport.Service
	Discards   *importdiscard.Service
	Admissions *libraryservice.ImportAdmissions
	Reads      *libraryservice.ImportReads
	Server     *serverimport.Service
	Source     *sourceimport.Service
}

type ReviewDependencies struct {
	Queue         *libraryservice.ReviewQueue
	Details       *libraryservice.ReviewDetails
	AssetUploads  *libraryservice.ReviewAssetUploads
	Discards      *libraryservice.ReviewDiscards
	Approvals     *libraryservice.ReviewApprovals
	BulkApprovals *libraryservice.ReviewBulk
	Screenshots   *libraryservice.ScreenshotSaver
	Previews      *libraryservice.ReviewPreviews
	Metadata      *metadatascrape.Service
	Evidence      *metadatascrape.EvidenceQueries
}

type PlayDependencies struct {
	Launcher        *launchservice.Service
	Variants        *gamevariant.Service
	Saves           *saves.Service
	Isolation       *isolation.Service
	RuntimeSessions *runtimesession.Service
	Provider        http.Handler
}

type SystemDependencies struct {
	Readiness   *readinessservice.Service
	Jobs        *jobs.Service
	Diagnostics *diagnosticsservice.Service
	Idempotency *idempotencyservice.Service
	Cleanup     CleanupNotifier
}

type ContentDependencies struct {
	Files       *filestore.Store
	Credentials *retromruntime.Credentials
	Access      *mediaaccess.Service
}
