package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"retrom/internal/config"
	"retrom/internal/cursor"
	"retrom/internal/service/accounts"
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
	errInvalidCursorPayload = errors.New("invalid cursor payload")
	errInvalidGameTagFilter = errors.New("invalid game tag filter")
	errTagProjectionType    = errors.New("invalid tag projection identifier type")
)

type contextKey string

const requestIDKey contextKey = "request-id"

type Server struct {
	deferredWork            sync.WaitGroup
	config                  config.Config
	startupReadinessMu      sync.Mutex
	startupReady            atomic.Bool
	cursors                 *cursor.Codec
	now                     func() time.Time
	sseHeartbeat            time.Duration
	idempotency             sync.Mutex
	idempotencyQueueMu      sync.Mutex
	idempotencyQueueWaiters int
	idempotencyQueueDrained *sync.Cond
	accountDeps             AccountDependencies
	libraryDeps             LibraryDependencies
	importDeps              ImportDependencies
	reviewDeps              ReviewDependencies
	playDeps                PlayDependencies
	systemDeps              SystemDependencies
	contentDeps             ContentDependencies
}

type Authenticator interface {
	Authenticate(context.Context, string) (accounts.Session, error)
}

func New(settings config.Config, deps Dependencies, now func() time.Time) *Server {
	server := &Server{
		config: settings, now: now,
		cursors:      cursor.New(deps.Content.Credentials.CursorKey(), now),
		sseHeartbeat: 15 * time.Second,
		accountDeps:  deps.Account,
		libraryDeps:  deps.Library,
		importDeps:   deps.Import,
		reviewDeps:   deps.Review,
		playDeps:     deps.Play,
		systemDeps:   deps.System,
		contentDeps:  deps.Content,
	}
	if server.playDeps.Provider == nil {
		server.playDeps.Provider = http.NotFoundHandler()
	}
	server.idempotencyQueueDrained = sync.NewCond(&server.idempotencyQueueMu)
	return server
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
	return server.routeByRuntimeHost(server.baseMiddleware(server.openAPIHandler(mux), mux))
}

func (server *Server) registerPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", server.healthLive)
	mux.HandleFunc("GET /health/ready", server.healthReady)
	mux.HandleFunc("GET /api/v1/web-config", server.webConfig)
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
	mux.HandleFunc("POST /api/v1/launches/{launchId}/local-save", server.createLocalGameSave)
	mux.HandleFunc("PATCH /api/v1/saves/{saveStateId}", server.patchSave)
	mux.HandleFunc("DELETE /api/v1/saves/{saveStateId}", server.deleteSave)
	mux.HandleFunc("POST /api/v1/launches", server.createLaunch)
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
	mux.HandleFunc("GET /api/v1/admin/runtime-targets", server.runtimeTargets)
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
	mux.HandleFunc("GET /api/v1/admin/import-batches/{kind}/{importId}/discard", server.getImportBatchDiscard)
	mux.HandleFunc("POST /api/v1/admin/import-batches/{kind}/{importId}/discard", server.discardImportBatch)
	mux.HandleFunc("POST /api/v1/admin/source-imports", server.createSourceImport)
	mux.HandleFunc("GET /api/v1/admin/source-imports", server.sourceImportList)
	mux.HandleFunc("GET /api/v1/admin/source-imports/{sourceImportId}", server.sourceImportDetail)
	mux.HandleFunc("DELETE /api/v1/admin/source-imports/{sourceImportId}", server.deleteSourceImport)
	mux.HandleFunc("GET /api/v1/admin/source-imports/{sourceImportId}/collections", server.sourceImportCollections)
	mux.HandleFunc(
		"PUT /api/v1/admin/source-imports/{sourceImportId}/collection-mappings",
		server.updateSourceMappings,
	)
	mux.HandleFunc("POST /api/v1/admin/source-imports/{sourceImportId}/start", server.startSourceImport)
	mux.HandleFunc("GET /api/v1/admin/source-imports/{sourceImportId}/items", server.sourceImportItems)
	mux.HandleFunc("POST /api/v1/admin/source-imports/{sourceImportId}/retry", server.retrySourceImport)
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
	mux.HandleFunc("POST /api/v1/admin/reviews/deduplicate", server.deduplicateReviews)
	mux.HandleFunc("POST /api/v1/admin/review-bulk-approvals", server.createReviewBulk)
	mux.HandleFunc("GET /api/v1/admin/review-bulk-approvals/active", server.activeReviewBulk)
	mux.HandleFunc("GET /api/v1/admin/review-bulk-approvals/{bulkApprovalId}", server.reviewBulk)
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
		{"POST /api/v1/admin/games/{gameId}/content-replacement", server.createGameContentReplacement},
		{"GET /api/v1/admin/games/{gameId}/scrape-candidates", server.gameScrapeCandidates},
		{"POST /api/v1/admin/games/{gameId}/scrape-candidates", server.scrapeGame},
		{"POST /api/v1/admin/games/{gameId}/scrape-candidates/{candidateId}/apply", server.applyGameScrapeCandidate},
		{"POST /api/v1/admin/games/{gameId}/move-preview", server.previewGameMove},
		{"POST /api/v1/admin/games/{gameId}/move", server.moveGame},
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
	mux.Handle("GET /runtime/providers/{providerId}/{bundleSha256}/{runtimePath...}", server.playDeps.Provider)
	mux.Handle("HEAD /runtime/providers/{providerId}/{bundleSha256}/{runtimePath...}", server.playDeps.Provider)
}

func (server *Server) registerRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /runtime/launches/{launchId}/config", server.launchConfig)
	mux.HandleFunc("POST /runtime/launches/{launchId}/renew", server.renewRuntimeSession)
	mux.HandleFunc("GET /runtime/content/project/{contentIdentity}/{projectPath...}", server.launchProjectFile)
	mux.HandleFunc("HEAD /runtime/content/project/{contentIdentity}/{projectPath...}", server.launchProjectFile)
	mux.HandleFunc("GET /runtime/content/web/{contentIdentity}/{projectPath...}", server.launchWebContent)
	mux.HandleFunc("HEAD /runtime/content/web/{contentIdentity}/{projectPath...}", server.launchWebContent)
	mux.HandleFunc("GET /runtime/content/game/{contentIdentity}/{logicalName}", server.launchGame)
	mux.HandleFunc("HEAD /runtime/content/game/{contentIdentity}/{logicalName}", server.launchGame)
	mux.HandleFunc("GET /runtime/content/external/{contentIdentity}/{logicalName}", server.launchExternalFile)
	mux.HandleFunc("HEAD /runtime/content/external/{contentIdentity}/{logicalName}", server.launchExternalFile)
	mux.HandleFunc("GET /runtime/content/bios/{contentIdentity}/bundle.zip", server.launchBIOSBundle)
	mux.HandleFunc("HEAD /runtime/content/bios/{contentIdentity}/bundle.zip", server.launchBIOSBundle)
	mux.HandleFunc("GET /runtime/content/parent/{contentIdentity}/bundle.zip", server.launchParentBundle)
	mux.HandleFunc("HEAD /runtime/content/parent/{contentIdentity}/bundle.zip", server.launchParentBundle)
	mux.HandleFunc("POST /runtime/launches/{launchId}/progress", server.launchProgress)
	mux.HandleFunc("POST /runtime/launches/{launchId}/finish", server.launchFinish)
	mux.HandleFunc("POST /runtime/launches/{launchId}/player-events", server.multiDiscPlayerEvent)
	mux.HandleFunc("POST /runtime/launches/{launchId}/save-states", server.createSaveState)
	mux.HandleFunc("GET /runtime/launches/{launchId}/checkpoint-status", server.checkpointStatus)
	mux.HandleFunc("GET /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("POST /runtime/launches/{launchId}/review-screenshot", server.storeReviewScreenshot)
}

// Wait joins work scheduled by completed HTTP handlers. Stop and drain HTTP before calling it.
func (server *Server) Wait() { server.deferredWork.Wait() }
