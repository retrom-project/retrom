package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"retrom/internal/accounts"
	"retrom/internal/blobstore"
	"retrom/internal/config"
	"retrom/internal/cursor"
	"retrom/internal/dependencies"
	"retrom/internal/emulationstationimport"
	"retrom/internal/favorites"
	"retrom/internal/firmware"
	"retrom/internal/gamecontent"
	"retrom/internal/hasheous"
	"retrom/internal/immersive"
	"retrom/internal/jobs"
	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	"retrom/internal/metadatascrape"
	"retrom/internal/netplay"
	"retrom/internal/payloadrelease"
	"retrom/internal/pegasusimport"
	"retrom/internal/platforminstance"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/saves"
	"retrom/internal/serverimport"
	"retrom/internal/storageanalysis"
	"retrom/internal/tagging"
	"retrom/internal/uploads"
)

var (
	errUnknownQuery         = errors.New("unknown or repeated query")
	errInvalidLimit         = errors.New("invalid limit")
	errQueryTooLong         = errors.New("query too long")
	errInvalidTimeRange     = errors.New("invalid time range")
	errJSONContentType      = errors.New("content type must be application/json")
	errJSONUTF8             = errors.New("JSON is not valid UTF-8")
	errJSONTrailing         = errors.New("trailing JSON value")
	errJSONNesting          = errors.New("JSON nesting exceeds limit")
	errJSONObjectKey        = errors.New("JSON object key is not a string")
	errJSONDuplicateKey     = errors.New("duplicate JSON object key")
	errJSONDelimiter        = errors.New("invalid JSON delimiter")
	errJSONClosingDelimiter = errors.New("invalid JSON closing delimiter")
	errInvalidETag          = errors.New("invalid ETag")
	errStaleImpact          = errors.New("stale")
	errInvalidCore          = errors.New("invalid core")
	errCandidateMetadata    = errors.New("candidate metadata invalid")
	errCandidateAssetKind   = errors.New("candidate asset kind mismatch")
	errInvalidCursorPayload = errors.New("invalid cursor payload")
	errInvalidGameTagFilter = errors.New("invalid game tag filter")
	errTagProjectionType    = errors.New("invalid tag projection identifier type")
	errGamePagination       = errors.New("game pagination projection invalid")
)

type contextKey string

const requestIDKey contextKey = "request-id"

type Server struct {
	config                  config.Config
	database                *sql.DB
	readinessDatabase       *sql.DB
	startupReadinessMu      sync.Mutex
	startupReady            atomic.Bool
	dependencies            *dependencies.Set
	blobs                   *blobstore.Store
	credentials             *retromruntime.Credentials
	cursors                 *cursor.Codec
	uploads                 *uploads.Service
	importer                *libraryimport.Service
	launcher                *launch.Service
	jobService              *jobs.Service
	immersive               *immersive.Service
	firmware                *firmware.Service
	metadata                *metadatascrape.Service
	gameContent             *gamecontent.Service
	saveService             *saves.Service
	favoriteService         *favorites.Service
	tagService              *tagging.Service
	serverImports           *serverimport.Service
	pegasusImports          *pegasusimport.Service
	emulationStationImports *emulationstationimport.Service
	payloadReleases         *payloadrelease.Service
	platformDirectories     *platforminstance.Service
	storageAnalysis         *storageanalysis.Service
	now                     func() time.Time
	sseHeartbeat            time.Duration
	idempotency             sync.Mutex
	idempotencyQueueMu      sync.Mutex
	idempotencyQueueWaiters int
	idempotencyQueueDrained *sync.Cond
	authenticator           Authenticator
	accounts                *accounts.Service
	netplay                 *netplay.Service
	netplayHub              *netplay.Hub
	netplayObserversMu      sync.Mutex
	netplayObservers        map[string]int
}

func (server *Server) WithNetplay(service *netplay.Service) *Server {
	server.netplay = service
	server.netplayHub = netplay.NewHub(service)
	service.StartMaintenance()
	return server
}

func (server *Server) WithReadinessDatabase(database *sql.DB) *Server {
	if database != nil {
		server.readinessDatabase = database
		server.storageAnalysis = storageanalysis.New(database, server.now)
	}
	return server
}

type Authenticator interface {
	Authenticate(context.Context, string) (accounts.Session, error)
}

func New(
	config config.Config,
	database *sql.DB,
	dependencySet *dependencies.Set,
	blobs *blobstore.Store,
	credentials *retromruntime.Credentials,
	authenticator Authenticator,
	accountService *accounts.Service,
	now func() time.Time,
) *Server {
	payloadReleaseService, err := payloadrelease.New(database, blobs, now, 7*24*time.Hour)
	if err != nil {
		panic(err)
	}
	scraper := metadatascrape.New(database, blobs, hasheous.New(nil, nil, now), now)
	launcher := launch.New(database, dependencySet, credentials, now).WithBlobStore(blobs)
	launcher.ResumeQueuedValidationJobs()
	importer := libraryimport.New(database, now, scraper).
		WithBlobStore(blobs).
		WithMultiDiscImportEnabled(config.MultiDiscImportEnabled)
	importer.ResumeParentAttachmentJobs(context.Background())
	importer.ResumeMultiDiscAttachmentJobs(context.Background())
	importer.ResumeReviewBulkJobs(context.Background())
	firmwareService := firmware.New(database, now).WithBlobStore(blobs).
		WithPayloadRelease(payloadReleaseService)
	serverImportService := serverimport.New(
		database,
		blobs,
		firmwareService,
		credentials,
		config.ServerImportRoots,
		now,
	)
	serverImportService.Start()
	pegasusImportService := pegasusimport.New(database, blobs, importer, credentials, config.ServerImportRoots, now)
	pegasusImportService.Start()
	emulationStationImportService := emulationstationimport.New(
		database, blobs, importer, credentials, config.ServerImportRoots, now,
	)
	emulationStationImportService.Start()
	server := &Server{
		config:                  config,
		database:                database,
		readinessDatabase:       database,
		dependencies:            dependencySet,
		blobs:                   blobs,
		credentials:             credentials,
		authenticator:           authenticator,
		accounts:                accountService,
		cursors:                 cursor.New(credentials.CursorKey(), now),
		uploads:                 uploads.New(database, blobs, config.DataDir, now),
		importer:                importer,
		launcher:                launcher,
		jobService:              jobs.New(database, now),
		immersive:               immersive.New(database),
		firmware:                firmwareService,
		serverImports:           serverImportService,
		pegasusImports:          pegasusImportService,
		emulationStationImports: emulationStationImportService,
		payloadReleases:         payloadReleaseService,
		platformDirectories:     platforminstance.New(database, now),
		metadata:                scraper,
		gameContent: gamecontent.New(database, now).WithBlobStore(blobs).
			WithPayloadRelease(payloadReleaseService).
			WithMultiDiscImportEnabled(config.MultiDiscImportEnabled),
		saveService:      saves.New(database, blobs, credentials, now),
		favoriteService:  favorites.New(database, now),
		tagService:       tagging.New(database, now),
		now:              now,
		sseHeartbeat:     15 * time.Second,
		netplayObservers: make(map[string]int),
	}
	server.idempotencyQueueDrained = sync.NewCond(&server.idempotencyQueueMu)
	payloadReleaseService.Start()
	return server
}

func (server *Server) Close() {
	if server.netplay != nil {
		server.netplayHub.Close()
		server.netplay.Close()
	}
	server.serverImports.Close()
	server.pegasusImports.Close()
	server.emulationStationImports.Close()
	server.payloadReleases.Close()
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	server.registerPublicRoutes(mux)
	server.registerAdminAccountRoutes(mux)
	server.registerAdminImportRoutes(mux)
	server.registerAdminLibraryRoutes(mux)
	server.registerContentRoutes(mux)
	server.registerRuntimeRoutes(mux)
	mux.HandleFunc("/", server.notFound)
	return server.baseMiddleware(server.openAPIHandler(mux))
}

func (server *Server) registerPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", server.healthLive)
	mux.HandleFunc("GET /health/ready", server.healthReady)
	mux.HandleFunc("GET /api/v1/auth/context", server.authContext)
	mux.HandleFunc("POST /api/v1/auth/initialize", server.authInitialize)
	mux.HandleFunc("POST /api/v1/auth/login", server.authLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", server.authLogout)
	mux.HandleFunc("POST /api/v1/auth/change-password", server.authChangePassword)
	mux.HandleFunc("POST /api/v1/auth/account-links/inspect", server.authAccountLinkInspect)
	mux.HandleFunc("POST /api/v1/auth/invitations/accept", server.authInvitationAccept)
	mux.HandleFunc("POST /api/v1/auth/password-resets/complete", server.authPasswordResetComplete)
	mux.HandleFunc("GET /api/v1/home", server.home)
	mux.HandleFunc("GET /api/v1/recent-games", server.recentGames)
	mux.HandleFunc("GET /api/v1/games", server.games)
	mux.HandleFunc("GET /api/v1/games/{gameId}", server.game)
	mux.HandleFunc("GET /api/v1/immersive/platforms", server.immersivePlatforms)
	mux.HandleFunc("GET /api/v1/immersive/platforms/{platformId}/games", server.immersivePlatformGames)
	mux.HandleFunc("GET /api/v1/immersive/destinations", server.immersiveDestinations)
	mux.HandleFunc("GET /api/v1/immersive/libraries/{libraryKind}/games", server.immersiveLibraryGames)
	mux.HandleFunc("GET /api/v1/favorites", server.favoritesList)
	mux.HandleFunc("PUT /api/v1/favorites/{gameId}", server.putFavorite)
	mux.HandleFunc("PUT /api/v1/favorites/{gameId}/folders", server.putFavoriteFolders)
	mux.HandleFunc("POST /api/v1/favorites/organize", server.organizeFavorites)
	mux.HandleFunc("POST /api/v1/favorites/unfavorite", server.unfavorite)
	mux.HandleFunc("POST /api/v1/favorites/restore", server.restoreFavorites)
	mux.HandleFunc("POST /api/v1/favorite-folders", server.createFavoriteFolder)
	mux.HandleFunc("PATCH /api/v1/favorite-folders/{folderId}", server.patchFavoriteFolder)
	mux.HandleFunc("DELETE /api/v1/favorite-folders/{folderId}", server.deleteFavoriteFolder)
	mux.HandleFunc("GET /api/v1/saves", server.saves)
	mux.HandleFunc("PATCH /api/v1/saves/{saveStateId}", server.patchSave)
	mux.HandleFunc("DELETE /api/v1/saves/{saveStateId}", server.deleteSave)
	mux.HandleFunc("POST /api/v1/launches", server.createLaunch)
	if server.config.NetplayEnabled {
		server.registerNetplayRoutes(mux)
	}
}

func (server *Server) registerAdminAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/invitations", server.adminInvitations)
	mux.HandleFunc("POST /api/v1/admin/invitations", server.adminCreateInvitation)
	mux.HandleFunc("GET /api/v1/admin/users", server.adminUsers)
	mux.HandleFunc("GET /api/v1/admin/users/{userId}", server.adminUser)
	mux.HandleFunc("PATCH /api/v1/admin/users/{userId}", server.adminPatchUser)
	mux.HandleFunc("DELETE /api/v1/admin/users/{userId}", server.adminDeleteUser)
	mux.HandleFunc(
		"GET /api/v1/admin/users/{userId}/password-reset-links",
		server.adminPasswordResetLinks,
	)
	mux.HandleFunc(
		"POST /api/v1/admin/users/{userId}/password-reset-links",
		server.adminCreatePasswordReset,
	)
	mux.HandleFunc("DELETE /api/v1/admin/account-links/{accountLinkId}", server.adminRevokeAccountLink)
	mux.HandleFunc("GET /api/v1/admin/platforms", server.platforms)
	mux.HandleFunc("GET /api/v1/admin/core-artifacts", server.coreArtifacts)
	mux.HandleFunc("GET /api/v1/admin/platform-instances", server.platformInstances)
	mux.HandleFunc("POST /api/v1/admin/platform-instances", server.createPlatformInstance)
	mux.HandleFunc(
		"GET /api/v1/admin/platform-instances/recommendations",
		server.platformInstanceRecommendations,
	)
	mux.HandleFunc(
		"POST /api/v1/admin/platform-instances/recommendations/apply",
		server.applyPlatformInstanceRecommendations,
	)
	mux.HandleFunc("PUT /api/v1/admin/platform-instances/order", server.reorderPlatformInstances)
	mux.HandleFunc("GET /api/v1/admin/platform-instances/{platformInstanceId}", server.platformInstance)
	mux.HandleFunc("PATCH /api/v1/admin/platform-instances/{platformInstanceId}", server.patchPlatformInstance)
	mux.HandleFunc("DELETE /api/v1/admin/platform-instances/{platformInstanceId}", server.deletePlatformInstance)
	mux.HandleFunc(
		"POST /api/v1/admin/platform-instances/{platformInstanceId}/default-core-preview",
		server.previewDefaultCore,
	)
	mux.HandleFunc("POST /api/v1/admin/platform-instances/{platformInstanceId}/default-core", server.changeDefaultCore)
	mux.HandleFunc("GET /api/v1/admin/bios", server.bios)
	mux.HandleFunc("GET /api/v1/admin/bios/{requirementId}/entries", server.biosEntries)
	mux.HandleFunc("POST /api/v1/admin/bios/{requirementId}/installations", server.installBIOS)
}

func (server *Server) registerAdminImportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/server-import-roots", server.serverImportRoots)
	mux.HandleFunc("GET /api/v1/admin/server-import-roots/{rootId}/directories", server.serverImportDirectories)
	mux.HandleFunc("POST /api/v1/admin/server-imports", server.createServerImport)
	mux.HandleFunc("GET /api/v1/admin/server-imports", server.serverImportList)
	mux.HandleFunc("GET /api/v1/admin/server-imports/{serverImportId}", server.serverImportDetail)
	mux.HandleFunc(
		"GET /api/v1/admin/server-imports/{serverImportId}/bios-items/{requirementId}/candidates",
		server.serverImportCandidates,
	)
	mux.HandleFunc("POST /api/v1/admin/server-imports/{serverImportId}/cancel", server.cancelServerImport)
	mux.HandleFunc("POST /api/v1/admin/server-imports/{serverImportId}/retry", server.retryServerImport)
	mux.HandleFunc("POST /api/v1/admin/pegasus-imports", server.createPegasusImport)
	mux.HandleFunc("GET /api/v1/admin/pegasus-imports", server.pegasusImportList)
	mux.HandleFunc("GET /api/v1/admin/pegasus-imports/{pegasusImportId}", server.pegasusImportDetail)
	mux.HandleFunc("DELETE /api/v1/admin/pegasus-imports/{pegasusImportId}", server.deletePegasusImport)
	mux.HandleFunc("GET /api/v1/admin/pegasus-imports/{pegasusImportId}/collections", server.pegasusImportCollections)
	mux.HandleFunc(
		"PUT /api/v1/admin/pegasus-imports/{pegasusImportId}/collection-mappings",
		server.updatePegasusMappings,
	)
	mux.HandleFunc("POST /api/v1/admin/pegasus-imports/{pegasusImportId}/start", server.startPegasusImport)
	mux.HandleFunc("GET /api/v1/admin/pegasus-imports/{pegasusImportId}/items", server.pegasusImportItems)
	mux.HandleFunc("POST /api/v1/admin/pegasus-imports/{pegasusImportId}/cancel", server.cancelPegasusImport)
	mux.HandleFunc("POST /api/v1/admin/pegasus-imports/{pegasusImportId}/retry", server.retryPegasusImport)
	mux.HandleFunc("POST /api/v1/admin/emulationstation-imports", server.createEmulationStationImport)
	mux.HandleFunc("GET /api/v1/admin/emulationstation-imports", server.emulationStationImportList)
	mux.HandleFunc(
		"GET /api/v1/admin/emulationstation-imports/{emulationStationImportId}",
		server.emulationStationImportDetail,
	)
	mux.HandleFunc(
		"DELETE /api/v1/admin/emulationstation-imports/{emulationStationImportId}",
		server.deleteEmulationStationImport,
	)
	mux.HandleFunc(
		"GET /api/v1/admin/emulationstation-imports/{emulationStationImportId}/gamelists",
		server.emulationStationImportGamelists,
	)
	mux.HandleFunc(
		"GET /api/v1/admin/emulationstation-imports/{emulationStationImportId}/collections",
		server.emulationStationImportCollections,
	)
	mux.HandleFunc(
		"PUT /api/v1/admin/emulationstation-imports/{emulationStationImportId}/collection-mappings",
		server.updateEmulationStationMappings,
	)
	mux.HandleFunc(
		"POST /api/v1/admin/emulationstation-imports/{emulationStationImportId}/start",
		server.startEmulationStationImport,
	)
	mux.HandleFunc(
		"GET /api/v1/admin/emulationstation-imports/{emulationStationImportId}/items",
		server.emulationStationImportItems,
	)
	mux.HandleFunc(
		"POST /api/v1/admin/emulationstation-imports/{emulationStationImportId}/cancel",
		server.cancelEmulationStationImport,
	)
	mux.HandleFunc(
		"POST /api/v1/admin/emulationstation-imports/{emulationStationImportId}/retry",
		server.retryEmulationStationImport,
	)
	mux.HandleFunc("GET /api/v1/admin/imports/summary", server.importSummary)
	mux.HandleFunc("GET /api/v1/admin/imports", server.imports)
	mux.HandleFunc("POST /api/v1/admin/imports", server.createImport)
	mux.HandleFunc("GET /api/v1/admin/imports/{importJobId}", server.importDetail)
	mux.HandleFunc("GET /api/v1/admin/imports/{importJobId}/events", server.importEvents)
	mux.HandleFunc("POST /api/v1/admin/imports/{importJobId}/cancel", server.cancelImport)
	mux.HandleFunc("POST /api/v1/admin/imports/{importJobId}/reconfigure", server.reconfigureImport)
	mux.HandleFunc("POST /api/v1/admin/import-items/{importItemId}/retry", server.retryImportItem)
	mux.HandleFunc("POST /api/v1/admin/uploads", server.createUpload)
	mux.HandleFunc("GET /api/v1/admin/uploads/{uploadId}", server.getUpload)
	mux.HandleFunc("DELETE /api/v1/admin/uploads/{uploadId}", server.cancelUpload)
	mux.HandleFunc("PUT /api/v1/admin/uploads/{uploadId}/files/{fileId}/parts/{partNo}", server.putUploadPart)
	mux.HandleFunc("POST /api/v1/admin/uploads/{uploadId}/complete", server.completeUpload)
	mux.HandleFunc("GET /api/v1/admin/jobs/{jobId}", server.job)
	mux.HandleFunc("GET /api/v1/admin/jobs/{jobId}/events", server.jobEvents)
	mux.HandleFunc("POST /api/v1/admin/jobs/{jobId}/cancel", server.cancelJob)
	mux.HandleFunc("POST /api/v1/admin/jobs/{jobId}/retry", server.retryJob)
	mux.HandleFunc("GET /api/v1/admin/reviews", server.reviews)
	mux.HandleFunc("GET /api/v1/admin/review-bulk-approval-preview", server.reviewBulkPreview)
	mux.HandleFunc("POST /api/v1/admin/review-bulk-approvals", server.createReviewBulk)
	mux.HandleFunc("GET /api/v1/admin/review-bulk-approvals/{bulkApprovalId}", server.reviewBulk)
	mux.HandleFunc("GET /api/v1/admin/review-bulk-approvals/{bulkApprovalId}/items", server.reviewBulkItems)
	mux.HandleFunc("POST /api/v1/admin/review-bulk-approvals/{bulkApprovalId}/cancel", server.cancelReviewBulk)
	mux.HandleFunc("POST /api/v1/admin/review-bulk-approvals/{bulkApprovalId}/retry", server.retryReviewBulk)
	mux.HandleFunc("GET /api/v1/admin/reviews/{importItemId}", server.review)
	mux.HandleFunc("PATCH /api/v1/admin/reviews/{importItemId}", server.patchReview)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/scrape-candidates", server.scrapeReview)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/assets", server.createReviewAsset)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/previews", server.createReviewPreview)
	mux.HandleFunc(
		"POST /api/v1/admin/reviews/{importItemId}/arcade-parent-attachments",
		server.createReviewArcadeParentAttachment,
	)
	mux.HandleFunc(
		"POST /api/v1/admin/reviews/{importItemId}/multi-disc-attachments",
		server.createReviewMultiDiscAttachment,
	)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/approve", server.approveReview)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/discard", server.discardReview)
	mux.HandleFunc("GET /api/v1/admin/review-history", server.reviewHistory)
	mux.HandleFunc("GET /api/v1/admin/review-history/{reviewEventId}", server.reviewHistoryEvent)
}

func (server *Server) registerAdminLibraryRoutes(mux *http.ServeMux) {
	routes := []struct {
		pattern string
		handler http.HandlerFunc
	}{
		{"GET /api/v1/admin/tags", server.adminTags},
		{"POST /api/v1/admin/tags", server.createAdminTag},
		{"POST /api/v1/admin/tags/defaults", server.applyAdminTagDefaults},
		{"GET /api/v1/admin/tags/{tagId}", server.adminTag},
		{"PATCH /api/v1/admin/tags/{tagId}", server.patchAdminTag},
		{"DELETE /api/v1/admin/tags/{tagId}", server.deleteAdminTag},
		{"GET /api/v1/admin/games", server.adminGames},
		{"GET /api/v1/admin/games/{gameId}", server.adminGame},
		{"PATCH /api/v1/admin/games/{gameId}", server.patchAdminGame},
		{"DELETE /api/v1/admin/games/{gameId}", server.deleteAdminGame},
		{"PUT /api/v1/admin/games/{gameId}/tags", server.putAdminGameTags},
		{"POST /api/v1/admin/games/{gameId}/assets", server.createGameAsset},
		{"DELETE /api/v1/admin/games/{gameId}/assets/{assetKind}", server.deleteGameAsset},
		{"POST /api/v1/admin/games/{gameId}/content-revisions", server.createGameContentRevision},
		{"GET /api/v1/admin/games/{gameId}/scrape-candidates", server.gameScrapeCandidates},
		{"POST /api/v1/admin/games/{gameId}/scrape-candidates", server.scrapeGame},
		{"POST /api/v1/admin/games/{gameId}/scrape-candidates/{candidateId}/apply", server.applyGameScrapeCandidate},
		{"POST /api/v1/admin/games/{gameId}/move-preview", server.previewGameMove},
		{"POST /api/v1/admin/games/{gameId}/move", server.moveGame},
		{"GET /api/v1/admin/storage-analysis", server.adminStorageAnalysis},
		{"POST /api/v1/admin/storage-cleanups", server.adminStorageCleanup},
	}
	for _, route := range routes {
		mux.HandleFunc(route.pattern, route.handler)
	}
}

func (server *Server) registerContentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /content/assets/{assetId}", server.contentAsset)
	mux.HandleFunc("HEAD /content/assets/{assetId}", server.contentAsset)
	mux.HandleFunc("GET /content/save-states/{saveStateId}/screenshot", server.saveStateScreenshot)
	mux.HandleFunc("HEAD /content/save-states/{saveStateId}/screenshot", server.saveStateScreenshot)
	mux.HandleFunc("GET /api/v1/admin/review-assets/{assetId}", server.reviewCandidateAsset)
	mux.HandleFunc("HEAD /api/v1/admin/review-assets/{assetId}", server.reviewCandidateAsset)
	mux.HandleFunc("GET /api/v1/admin/diagnostics", server.diagnostics)
	mux.HandleFunc("GET /runtime/emulatorjs/{configuredVersion}/{runtimePath...}", server.runtimeFile)
	mux.HandleFunc("HEAD /runtime/emulatorjs/{configuredVersion}/{runtimePath...}", server.runtimeFile)
}

func (server *Server) registerRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /runtime/launches/{launchId}/config", server.launchConfig)
	mux.HandleFunc("GET /runtime/content/game/{contentIdentity}/{logicalName}", server.launchGame)
	mux.HandleFunc("HEAD /runtime/content/game/{contentIdentity}/{logicalName}", server.launchGame)
	mux.HandleFunc("GET /runtime/content/external/{contentIdentity}/{logicalName}", server.launchExternalFile)
	mux.HandleFunc("HEAD /runtime/content/external/{contentIdentity}/{logicalName}", server.launchExternalFile)
	mux.HandleFunc("GET /runtime/content/bios/{contentIdentity}/bundle.zip", server.launchBIOSBundle)
	mux.HandleFunc("HEAD /runtime/content/bios/{contentIdentity}/bundle.zip", server.launchBIOSBundle)
	mux.HandleFunc("GET /runtime/content/parent/{contentIdentity}/bundle.zip", server.launchParentBundle)
	mux.HandleFunc("HEAD /runtime/content/parent/{contentIdentity}/bundle.zip", server.launchParentBundle)
	mux.HandleFunc("POST /runtime/launches/{launchId}/start", server.launchStart)
	mux.HandleFunc("POST /runtime/launches/{launchId}/heartbeat", server.launchHeartbeat)
	mux.HandleFunc("POST /runtime/launches/{launchId}/finish", server.launchFinish)
	mux.HandleFunc("POST /runtime/launches/{launchId}/player-events", server.multiDiscPlayerEvent)
	mux.HandleFunc("POST /runtime/launches/{launchId}/save-states", server.createSaveState)
	mux.HandleFunc("GET /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("POST /runtime/launches/{launchId}/review-screenshot", server.storeReviewScreenshot)
}
