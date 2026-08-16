package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/accounts"
	"retrom/internal/arcadecatalog"
	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
	"retrom/internal/cursor"
	"retrom/internal/dependencies"
	"retrom/internal/dosbundle"
	"retrom/internal/favorites"
	"retrom/internal/firmware"
	"retrom/internal/gamecontent"
	"retrom/internal/hasheous"
	"retrom/internal/jobs"
	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	"retrom/internal/mediaasset"
	"retrom/internal/metadatascrape"
	"retrom/internal/netplay"
	"retrom/internal/pegasusimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/saves"
	"retrom/internal/serverimport"
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
	firmware                *firmware.Service
	arcadeDAT               *arcadecatalog.Service
	metadata                *metadatascrape.Service
	gameContent             *gamecontent.Service
	saveService             *saves.Service
	favoriteService         *favorites.Service
	tagService              *tagging.Service
	serverImports           *serverimport.Service
	pegasusImports          *pegasusimport.Service
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
	scraper := metadatascrape.New(database, blobs, hasheous.New(nil, nil, now), now)
	launcher := launch.New(database, dependencySet, credentials, now).WithBlobStore(blobs)
	arcadeDAT := arcadecatalog.New(
		database,
		blobs,
		now,
		arcadecatalog.RevalidationHooks{
			Queue:  launcher.QueueDATRevalidations,
			Resume: launcher.ResumeQueuedValidationJobs,
		},
	)
	launcher.ResumeQueuedValidationJobs()
	arcadeDAT.ResumeDiffJobs()
	importer := libraryimport.New(database, now, scraper).
		WithBlobStore(blobs).
		WithMultiDiscImportEnabled(config.MultiDiscImportEnabled)
	importer.ResumeParentAttachmentJobs(context.Background())
	importer.ResumeMultiDiscAttachmentJobs(context.Background())
	importer.ResumeReviewBulkJobs(context.Background())
	firmwareService := firmware.New(database, now).WithBlobStore(blobs)
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
	server := &Server{
		config:            config,
		database:          database,
		readinessDatabase: database,
		dependencies:      dependencySet,
		blobs:             blobs,
		credentials:       credentials,
		authenticator:     authenticator,
		accounts:          accountService,
		cursors:           cursor.New(credentials.CursorKey(), now),
		uploads:           uploads.New(database, blobs, config.DataDir, now),
		importer:          importer,
		launcher:          launcher,
		jobService:        jobs.New(database, now),
		firmware:          firmwareService,
		serverImports:     serverImportService,
		pegasusImports:    pegasusImportService,
		arcadeDAT:         arcadeDAT,
		metadata:          scraper,
		gameContent: gamecontent.New(database, now).WithBlobStore(blobs).
			WithMultiDiscImportEnabled(config.MultiDiscImportEnabled),
		saveService:      saves.New(database, blobs, credentials, now),
		favoriteService:  favorites.New(database, now),
		tagService:       tagging.New(database, now),
		now:              now,
		sseHeartbeat:     15 * time.Second,
		netplayObservers: make(map[string]int),
	}
	server.idempotencyQueueDrained = sync.NewCond(&server.idempotencyQueueMu)
	return server
}

func (server *Server) Close() {
	if server.netplay != nil {
		server.netplayHub.Close()
		server.netplay.Close()
	}
	server.serverImports.Close()
	server.pegasusImports.Close()
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
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
	mux.HandleFunc("PUT /api/v1/admin/platform-instances/order", server.reorderPlatformInstances)
	mux.HandleFunc("GET /api/v1/admin/platform-instances/{platformInstanceId}", server.platformInstance)
	mux.HandleFunc("PATCH /api/v1/admin/platform-instances/{platformInstanceId}", server.patchPlatformInstance)
	mux.HandleFunc("DELETE /api/v1/admin/platform-instances/{platformInstanceId}", server.deletePlatformInstance)
	mux.HandleFunc(
		"POST /api/v1/admin/platform-instances/{platformInstanceId}/default-core-preview",
		server.previewDefaultCore,
	)
	mux.HandleFunc("POST /api/v1/admin/platform-instances/{platformInstanceId}/default-core", server.changeDefaultCore)
	mux.HandleFunc("GET /api/v1/admin/arcade-dats", server.arcadeDATs)
	mux.HandleFunc("POST /api/v1/admin/arcade-dats", server.createArcadeDAT)
	mux.HandleFunc("GET /api/v1/admin/arcade-dats/{datVersionId}/diff", server.arcadeDATDiff)
	mux.HandleFunc("POST /api/v1/admin/arcade-dats/{datVersionId}/diff", server.createArcadeDATDiff)
	mux.HandleFunc("POST /api/v1/admin/arcade-dats/{datVersionId}/activate", server.activateArcadeDAT)
	mux.HandleFunc("POST /api/v1/admin/arcade-dats/{datVersionId}/rollback", server.rollbackArcadeDAT)
	mux.HandleFunc("DELETE /api/v1/admin/arcade-dats/{datVersionId}", server.deleteArcadeDAT)
	mux.HandleFunc("GET /api/v1/admin/bios", server.bios)
	mux.HandleFunc("GET /api/v1/admin/bios/{requirementId}/entries", server.biosEntries)
	mux.HandleFunc("POST /api/v1/admin/bios/{requirementId}/installations", server.installBIOS)
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
	mux.HandleFunc("GET /api/v1/admin/tags", server.adminTags)
	mux.HandleFunc("POST /api/v1/admin/tags", server.createAdminTag)
	mux.HandleFunc("GET /api/v1/admin/tags/{tagId}", server.adminTag)
	mux.HandleFunc("PATCH /api/v1/admin/tags/{tagId}", server.patchAdminTag)
	mux.HandleFunc("DELETE /api/v1/admin/tags/{tagId}", server.deleteAdminTag)
	mux.HandleFunc("GET /api/v1/admin/games", server.adminGames)
	mux.HandleFunc("GET /api/v1/admin/games/{gameId}", server.adminGame)
	mux.HandleFunc("PATCH /api/v1/admin/games/{gameId}", server.patchAdminGame)
	mux.HandleFunc("DELETE /api/v1/admin/games/{gameId}", server.deleteAdminGame)
	mux.HandleFunc("PUT /api/v1/admin/games/{gameId}/tags", server.putAdminGameTags)
	mux.HandleFunc("POST /api/v1/admin/games/{gameId}/assets", server.createGameAsset)
	mux.HandleFunc("DELETE /api/v1/admin/games/{gameId}/assets/{assetKind}", server.deleteGameAsset)
	mux.HandleFunc("POST /api/v1/admin/games/{gameId}/content-revisions", server.createGameContentRevision)
	mux.HandleFunc("GET /api/v1/admin/games/{gameId}/scrape-candidates", server.gameScrapeCandidates)
	mux.HandleFunc("POST /api/v1/admin/games/{gameId}/scrape-candidates", server.scrapeGame)
	mux.HandleFunc(
		"POST /api/v1/admin/games/{gameId}/scrape-candidates/{candidateId}/apply",
		server.applyGameScrapeCandidate,
	)
	mux.HandleFunc("POST /api/v1/admin/games/{gameId}/move-preview", server.previewGameMove)
	mux.HandleFunc("POST /api/v1/admin/games/{gameId}/move", server.moveGame)
	mux.HandleFunc("GET /content/assets/{assetId}", server.contentAsset)
	mux.HandleFunc("HEAD /content/assets/{assetId}", server.contentAsset)
	mux.HandleFunc("GET /content/save-states/{saveStateId}/screenshot", server.saveStateScreenshot)
	mux.HandleFunc("HEAD /content/save-states/{saveStateId}/screenshot", server.saveStateScreenshot)
	mux.HandleFunc("GET /api/v1/admin/review-assets/{assetId}", server.reviewCandidateAsset)
	mux.HandleFunc("HEAD /api/v1/admin/review-assets/{assetId}", server.reviewCandidateAsset)
	mux.HandleFunc("GET /api/v1/admin/diagnostics", server.diagnostics)
	mux.HandleFunc("GET /runtime/emulatorjs/{configuredVersion}/{runtimePath...}", server.runtimeFile)
	mux.HandleFunc("HEAD /runtime/emulatorjs/{configuredVersion}/{runtimePath...}", server.runtimeFile)
	mux.HandleFunc("GET /runtime/launches/{launchId}/config", server.launchConfig)
	mux.HandleFunc("GET /runtime/launches/{launchId}/game/{logicalName}", server.launchGame)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/game/{logicalName}", server.launchGame)
	mux.HandleFunc("GET /runtime/launches/{launchId}/external-files/{logicalName}", server.launchExternalFile)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/external-files/{logicalName}", server.launchExternalFile)
	mux.HandleFunc("GET /runtime/launches/{launchId}/bios/bundle.zip", server.launchBIOSBundle)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/bios/bundle.zip", server.launchBIOSBundle)
	mux.HandleFunc("GET /runtime/launches/{launchId}/parent/bundle.zip", server.launchParentBundle)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/parent/bundle.zip", server.launchParentBundle)
	mux.HandleFunc("POST /runtime/launches/{launchId}/start", server.launchStart)
	mux.HandleFunc("POST /runtime/launches/{launchId}/heartbeat", server.launchHeartbeat)
	mux.HandleFunc("POST /runtime/launches/{launchId}/finish", server.launchFinish)
	mux.HandleFunc("POST /runtime/launches/{launchId}/player-events", server.multiDiscPlayerEvent)
	mux.HandleFunc("POST /runtime/launches/{launchId}/save-states", server.createSaveState)
	mux.HandleFunc("GET /runtime/launches/{launchId}/persistent-save", server.getPersistentSave)
	mux.HandleFunc("PUT /runtime/launches/{launchId}/persistent-save", server.putPersistentSave)
	mux.HandleFunc("GET /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("POST /runtime/launches/{launchId}/review-screenshot", server.storeReviewScreenshot)
	mux.HandleFunc("/", server.notFound)
	return server.baseMiddleware(server.openAPIHandler(mux))
}

//nolint:gocognit,gocyclo // One boundary applies readiness, origin, authentication, role, and CSRF in fixed order.
func (server *Server) baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID, err := uuid.NewV7()
		if err != nil {
			http.Error(writer, "request id unavailable", http.StatusInternalServerError)
			return
		}
		requestContext := context.WithValue(request.Context(), requestIDKey, requestID.String())
		request = request.WithContext(requestContext)
		setBaseResponseHeaders(writer, requestID.String())
		if !server.requestReady(requestContext, writer, request) {
			return
		}
		if err := validateQuery(request); err != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "查询参数无效", map[string]any{})
			return
		}
		if server.config.Mode == config.ModeRelease || server.config.Mode == config.ModeTest {
			if unsafeMethod(request.Method) && !server.validRequestOrigin(request) {
				writeError(writer, request, http.StatusForbidden, "REQUEST_ORIGIN_INVALID", "请求来源无效", map[string]any{})
				return
			}
		}
		if publicHTTPRoute(request) || launchHTTPRoute(request.URL.Path) {
			next.ServeHTTP(writer, request)
			return
		}
		if server.accounts != nil {
			contextView, contextErr := server.accounts.Context(requestContext, server.authCookieToken(request))
			if contextErr != nil {
				server.databaseError(writer, request, contextErr)
				return
			}
			if contextView.InstanceState == "INITIALIZATION_REQUIRED" {
				writeError(
					writer,
					request,
					http.StatusPreconditionRequired,
					"INITIALIZATION_REQUIRED",
					"实例尚未初始化",
					map[string]any{},
				)
				return
			}
		}
		if server.authenticator == nil {
			writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要登录", map[string]any{})
			return
		}
		session, authErr := server.authenticator.Authenticate(requestContext, server.authCookieToken(request))
		if authErr != nil {
			server.clearAuthCookies(writer)
			writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要登录", map[string]any{})
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/v1/admin/") && session.Principal.Role != "ADMIN" {
			writeError(writer, request, http.StatusForbidden, "ADMIN_REQUIRED", "需要管理员权限", map[string]any{})
			return
		}
		if (server.config.Mode == config.ModeRelease || server.config.Mode == config.ModeTest) &&
			unsafeMethod(request.Method) &&
			!accounts.MatchesCSRF(session.CookieToken, request.Header.Get("X-Retrom-Csrf")) {
			writeError(writer, request, http.StatusForbidden, "CSRF_VALIDATION_FAILED", "请求验证失败", map[string]any{})
			return
		}
		request = request.WithContext(authn.WithPrincipal(requestContext, session.Principal))
		next.ServeHTTP(writer, request)
	})
}

func setBaseResponseHeaders(writer http.ResponseWriter, requestID string) {
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	writer.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
}

func (server *Server) requestReady(
	requestContext context.Context,
	writer http.ResponseWriter,
	request *http.Request,
) bool {
	if request.URL.Path == "/health/live" || request.URL.Path == "/health/ready" {
		return true
	}
	if server.startupReady.Load() {
		return true
	}
	server.startupReadinessMu.Lock()
	defer server.startupReadinessMu.Unlock()
	if server.startupReady.Load() {
		return true
	}
	reason := server.readinessReason(requestContext)
	if reason == "" {
		server.startupReady.Store(true)
		return true
	}
	writeError(
		writer,
		request,
		http.StatusServiceUnavailable,
		"SERVICE_NOT_READY",
		"依赖索引尚未就绪",
		map[string]any{"reasonCode": reason},
	)
	return false
}

func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func publicHTTPRoute(request *http.Request) bool {
	path := request.URL.Path
	if path == "/health/live" || path == "/health/ready" || path == "/api/v1/auth/context" ||
		path == "/api/v1/auth/initialize" || path == "/api/v1/auth/login" || path == "/api/v1/auth/logout" ||
		path == "/api/v1/auth/account-links/inspect" || path == "/api/v1/auth/invitations/accept" ||
		path == "/api/v1/auth/password-resets/complete" {
		return true
	}
	return (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
		strings.HasPrefix(path, "/runtime/emulatorjs/")
}

func launchHTTPRoute(path string) bool { return strings.HasPrefix(path, "/runtime/launches/") }

func (server *Server) validRequestOrigin(request *http.Request) bool {
	values := request.Header.Values("Origin")
	if len(values) != 1 || values[0] != server.config.PublicOrigin.String() {
		return false
	}
	if fetchSite := request.Header.Get("Sec-Fetch-Site"); fetchSite != "" && fetchSite != "same-origin" {
		return false
	}
	return true
}

var exactQueryAllowlists = map[string][]string{
	"GET /api/v1/games":     {"q", "tagId", "platformId", "platformInstanceId", "sort", "cursor", "limit"},
	"GET /api/v1/favorites": {"scope", "folderId", "q", "platformId", "sort", "cursor", "limit"},
	"GET /api/v1/saves": {
		"q", "gameId", "platformId", "platformInstanceId", "coreId", "availability", "sort", "cursor", "limit",
	},
	"GET /api/v1/netplay/games": {"cursor", "limit", "availability"},
	"GET /api/v1/netplay/rooms": {"view", "cursor", "limit"},
	"GET /api/v1/admin/imports": {"q", "state", "platformInstanceId", "sort", "cursor", "limit"},
	"GET /api/v1/admin/reviews": {
		"q", "tagId", "importJobId", "pegasusImportId", "platformInstanceId", "blockerCode", "sort", "cursor", "limit",
	},
	"GET /api/v1/admin/review-bulk-approval-preview": {
		"q", "tagId", "importJobId", "pegasusImportId", "platformInstanceId", "blockerCode",
	},
	"GET /api/v1/admin/review-history": {
		"q", "decision", "platformInstanceId", "fromAtMs", "toAtMs", "sort", "cursor", "limit",
	},
	"GET /api/v1/admin/games": {
		"q",
		"tagId",
		"platformId",
		"platformInstanceId",
		"status",
		"sort",
		"cursor",
		"limit",
	},
	"GET /api/v1/admin/tags":               {"q", "status", "sort", "cursor", "limit"},
	"GET /api/v1/admin/platform-instances": {"platformId", "enabled", "sort", "cursor", "limit"},
	"GET /api/v1/admin/bios": {
		"q", "platformId", "coreId", "coreArtifactId", "scope", "status", "quick", "cursor", "limit",
	},
	"GET /api/v1/admin/server-import-roots": {},
	"GET /api/v1/admin/server-imports":      {"kind", "state", "cursor", "limit"},
	"GET /api/v1/admin/pegasus-imports":     {"state", "cursor", "limit"},
	"GET /api/v1/admin/arcade-dats": {
		"q",
		"coreId",
		"coreArtifactId",
		"source",
		"parseStatus",
		"cursor",
		"limit",
	},
	"GET /api/v1/admin/users":       {"q", "role", "status", "sort", "cursor", "limit"},
	"GET /api/v1/admin/invitations": {"state", "cursor", "limit"},
}

func reviewBulkQueryParameterNames(method, path string) []string {
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/review-bulk-approvals/") &&
		strings.HasSuffix(path, "/items") {
		return []string{"outcome", "cursor", "limit"}
	}
	return nil
}

//nolint:gocyclo // The lexical query parser handles independent escaping and separator states.
func queryParameterNames(request *http.Request) []string {
	path := request.URL.Path
	if (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
		strings.HasPrefix(path, "/runtime/emulatorjs/") {
		return []string{"v"}
	}
	if (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
		strings.HasPrefix(path, "/api/v1/admin/review-assets/") {
		return []string{"kind"}
	}
	if names := exactQueryAllowlists[request.Method+" "+path]; names != nil {
		return names
	}
	if request.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/arcade-dats/") &&
		strings.HasSuffix(path, "/diff") {
		return []string{"section", "change", "cursor", "limit"}
	}
	if request.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/users/") &&
		strings.HasSuffix(path, "/password-reset-links") {
		return []string{"state", "cursor", "limit"}
	}
	if names := reviewBulkQueryParameterNames(request.Method, path); names != nil {
		return names
	}
	if request.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/server-import-roots/") &&
		strings.HasSuffix(path, "/directories") {
		return []string{"path", "cursor", "limit"}
	}
	if request.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/server-imports/") {
		if strings.Contains(path, "/bios-items/") && strings.HasSuffix(path, "/candidates") {
			return []string{"cursor", "limit"}
		}
		return []string{"q", "outcome", "matchMethod", "cursor", "limit"}
	}
	if request.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/pegasus-imports/") {
		if strings.HasSuffix(path, "/collections") {
			return []string{"cursor", "limit"}
		}
		if strings.HasSuffix(path, "/items") {
			return []string{"q", "outcome", "warning", "collectionId", "cursor", "limit"}
		}
		return []string{}
	}
	return nil
}

func queryAllowlist(request *http.Request) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, name := range queryParameterNames(request) {
		allowed[name] = struct{}{}
	}
	return allowed
}

func validateQueryLimit(values url.Values) error {
	if value := values.Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 || strconv.Itoa(parsed) != value {
			return errInvalidLimit
		}
	}
	return nil
}

func validateQueryTimeRange(values url.Values) error {
	if !values.Has("fromAtMs") || !values.Has("toAtMs") {
		return nil
	}
	from, fromErr := strconv.ParseInt(values.Get("fromAtMs"), 10, 64)
	to, toErr := strconv.ParseInt(values.Get("toAtMs"), 10, 64)
	if fromErr != nil || toErr != nil || from > to {
		return errInvalidTimeRange
	}
	return nil
}

func validateQueryValues(values url.Values, allowed map[string]struct{}) error {
	for name, entries := range values {
		if _, ok := allowed[name]; !ok || len(entries) != 1 {
			return errUnknownQuery
		}
	}
	if err := validateQueryLimit(values); err != nil {
		return err
	}
	if len(values.Get("cursor")) > 8192 || len([]rune(strings.TrimSpace(values.Get("q")))) > 200 {
		return errQueryTooLong
	}
	return validateQueryTimeRange(values)
}

func validateQuery(request *http.Request) error {
	return validateQueryValues(request.URL.Query(), queryAllowlist(request))
}

func (server *Server) healthLive(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ok"})
}

func (server *Server) healthReady(writer http.ResponseWriter, request *http.Request) {
	if reason := server.readinessReason(request.Context()); reason != "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reasonCode": reason})
		return
	}
	server.startupReady.Store(true)
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ready"})
}

func (server *Server) readinessReason(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	database := server.readinessDatabase
	if database == nil {
		database = server.database
	}
	if err := database.PingContext(ctx); err != nil {
		return "DATABASE_UNAVAILABLE"
	}
	var missing int64
	err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM core_artifacts a
WHERE a.enabled=1
AND a.core_id IN ('fbneo',
'mame2003',
'mame2003_plus')
AND NOT EXISTS(SELECT 1
FROM dat_versions d
WHERE d.core_artifact_id=a.id
AND d.is_active=1
AND d.parse_status='READY')
`).
		Scan(&missing)
	if err != nil {
		return "DATABASE_UNAVAILABLE"
	}
	if missing == 0 {
		return ""
	}
	var failed int64
	err = database.QueryRowContext(ctx, `
SELECT count(*)
FROM core_artifacts a
WHERE a.enabled=1
AND a.core_id IN ('fbneo',
'mame2003',
'mame2003_plus')
AND NOT EXISTS(SELECT 1
FROM dat_versions active
WHERE active.core_artifact_id=a.id
AND active.is_active=1
AND active.parse_status='READY')
AND EXISTS(SELECT 1
FROM dat_versions failed
WHERE failed.core_artifact_id=a.id
AND failed.source='BUILTIN'
AND failed.parse_status='FAILED')
`).
		Scan(&failed)
	if err != nil {
		return "DATABASE_UNAVAILABLE"
	}
	if failed > 0 {
		return "DEPENDENCY_DAT_PARSE_FAILED"
	}
	return "DEPENDENCY_INDEXING"
}

func queryWithConditions(prefix string, conditions []string, suffix string) string {
	if len(conditions) == 0 {
		return prefix + suffix
	}
	// Every condition is selected from handler-owned literals; all request values remain bound arguments.
	return prefix + " WHERE " + strings.Join(
		conditions,
		" AND ",
	) + suffix
}

//nolint:funlen,nestif // Contract branches stay contiguous for a single auditable decision.
func (server *Server) createLaunch(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body launch.CreateRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "启动请求无效", map[string]any{})
		return
	}
	canonical, _ := json.Marshal(body)
	digestBytes := sha256.Sum256(append([]byte("postLaunch\x00"+principal.UserID+"\x00"), canonical...))
	requestDigest := hex.EncodeToString(digestBytes[:])
	server.idempotency.Lock()
	defer server.idempotency.Unlock()
	var storedDigest string
	var storedStatus int
	var storedBody []byte
	err := server.database.QueryRowContext(request.Context(), `
SELECT request_digest,
http_status,
response_body
FROM idempotency_records
WHERE operation_id='postLaunch'
AND key=?
AND principal_id=?
`, key, principal.UserID).
		Scan(&storedDigest, &storedStatus, &storedBody)
	if err == nil {
		if subtle.ConstantTimeCompare([]byte(storedDigest), []byte(requestDigest)) != 1 {
			writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
			return
		}
		if storedStatus == http.StatusCreated {
			var replay struct {
				LaunchID string `json:"launchId"`
			}
			if json.Unmarshal(storedBody, &replay) == nil {
				server.setLaunchCookie(writer, replay.LaunchID)
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
		writer.WriteHeader(storedStatus)
		_, _ = writer.Write(storedBody)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		server.databaseError(writer, request, err)
		return
	}
	created, err := server.launcher.Create(request.Context(), principal.ProfileID, body)
	if err != nil {
		code := "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
		if errors.Is(err, launch.ErrDOSEntryMissing) {
			code = "LAUNCH_DOS_ENTRY_MISSING"
		}
		if errors.Is(err, launch.ErrDOSEntryUnsafe) {
			code = "LAUNCH_DOS_ENTRY_UNSAFE"
		}
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"LAUNCH_BLOCKED",
			"当前游戏或核心无法启动",
			map[string]any{"blockers": []map[string]any{{"code": code, "level": "BLOCKING"}}},
		)
		return
	}
	status := http.StatusCreated
	responseValue := any(created)
	if created.Status == "VALIDATION_PENDING" {
		status = http.StatusAccepted
		responseValue = map[string]any{
			"status":       created.Status,
			"jobId":        created.JobID,
			"retryAfterMs": created.RetryAfterMS,
		}
	} else {
		server.setLaunchCookie(writer, created.LaunchID)
	}
	responseBody, _ := json.Marshal(responseValue)
	now := server.now().UnixMilli()
	if _, err := server.database.ExecContext(request.Context(), `
INSERT INTO idempotency_records(principal_id,
operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms) VALUES(?,
'postLaunch',
?,
?,
?,
'{}',
?,
?,
?)
`,
		principal.UserID,
		key,
		requestDigest,
		status,
		responseBody,
		now,
		now+int64(24*time.Hour/time.Millisecond),
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(responseBody)
}

func (server *Server) setLaunchCookie(writer http.ResponseWriter, launchID string) {
	parsed, err := uuid.Parse(launchID)
	if err != nil || parsed.Version() != 7 {
		return
	}
	capability := server.credentials.Capability(parsed)
	http.SetCookie(
		writer,
		&http.Cookie{
			Name:     "retrom_launch_" + launchID,
			Value:    retromruntime.EncodeCapability(capability),
			Path:     "/runtime/launches/" + launchID + "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   server.config.PublicOrigin.Scheme == "https",
		},
	)
}

func (server *Server) createReviewPreview(writer http.ResponseWriter, request *http.Request) {
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		ClientCapabilities launch.Capabilities `json:"clientCapabilities"`
	}
	if err := decodeJSON(writer, request, &body, 16<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核预览请求无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	created, err := server.launcher.CreateReviewPreview(request.Context(), launch.ReviewPreviewRequest{
		ImportItemID: request.PathValue("importItemId"), ActorUserID: principal.UserID,
		IdempotencyKey: key, ClientCapabilities: body.ClientCapabilities,
	})
	if err != nil {
		code, message := "REVIEW_PREVIEW_UNAVAILABLE", "当前审核来源无法组成可运行预览"
		if errors.Is(err, launch.ErrBlocked) {
			code, message = "REVIEW_PREVIEW_CLIENT_UNSUPPORTED", "当前浏览器不满足该核心的运行要求"
		}
		writeError(writer, request, http.StatusUnprocessableEntity, code, message, map[string]any{
			"bestEffort": true,
		})
		return
	}
	server.setLaunchCookie(writer, created.PreviewID)
	writeJSON(writer, http.StatusCreated, created)
}

func (server *Server) launchCapability(request *http.Request) string {
	cookie, err := request.Cookie("retrom_launch_" + request.PathValue("launchId"))
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (server *Server) storeReviewScreenshot(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/png" {
		writeError(writer, request, http.StatusBadRequest, "REVIEW_SCREENSHOT_INVALID", "运行截图必须是 PNG", map[string]any{})
		return
	}
	body := http.MaxBytesReader(writer, request.Body, mediaasset.MaxImageBytes+1)
	result, err := server.launcher.StoreReviewScreenshot(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request), body,
	)
	if err != nil {
		if errors.Is(err, launch.ErrCredential) {
			writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "审核预览会话不可用", map[string]any{})
			return
		}
		if errors.Is(err, launch.ErrReviewCaptureNotAllowed) {
			writeError(
				writer, request, http.StatusConflict, "REVIEW_CAPTURE_NOT_ALLOWED",
				"只有运行检查通过的条目才能保存五秒截图", map[string]any{},
			)
			return
		}
		if errors.Is(err, launch.ErrReviewScreenshotInvalid) {
			writeError(writer, request, http.StatusBadRequest, "REVIEW_SCREENSHOT_INVALID", "运行截图无效或超过大小限制", map[string]any{})
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"screenshotId":   result.ID,
		"importItemId":   result.ImportItemID,
		"validationId":   result.ValidationID,
		"coreArtifactId": result.CoreArtifactID,
		"widthPx":        result.WidthPX,
		"heightPx":       result.HeightPX,
		"capturedAtMs":   result.CapturedAtMS,
		"url":            "/api/v1/admin/review-assets/" + result.ID,
	})
}

func (server *Server) launchConfig(writer http.ResponseWriter, request *http.Request) {
	capability := server.launchCapability(request)
	configuration, err := server.launcher.Config(
		request.Context(),
		request.PathValue("launchId"),
		capability,
	)
	if err != nil {
		configuration, err = server.launcher.ReviewPreviewConfig(
			request.Context(), request.PathValue("launchId"), capability,
		)
	}
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if configuration.DiscSet != nil {
		if dimensions, dimensionErr := server.launcher.MultiDiscTelemetryDimensions(
			request.Context(), request.PathValue("launchId"), capability,
		); dimensionErr == nil {
			logMultiDiscRuntime(
				request.Context(), request.PathValue("launchId"), dimensions.PlatformKey,
				dimensions.CoreKey, dimensions.ArtifactVersion, dimensions.DiscCount,
				"kind", "launch", "resultCode", "OK",
			)
		}
	}
	writer.Header().Set("Vary", "Cookie")
	writeJSON(writer, http.StatusOK, configuration)
}

func (server *Server) launchGame(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	content, err := server.runtimeContent(request)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动内容不可用", map[string]any{})
		return
	}
	isMultiDisc := content.Format == "RETROM_MULTIDISC_M3U_V1" && content.DiscCount >= 2
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		if isMultiDisc {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
				content.ArtifactVersion, content.DiscCount, "PLAYLIST", http.StatusServiceUnavailable, 0,
				"CAS_UNAVAILABLE",
			)
		}
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	stat, err := file.Stat()
	if err != nil {
		if isMultiDisc {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
				content.ArtifactVersion, content.DiscCount, "PLAYLIST", http.StatusServiceUnavailable, 0,
				"CAS_UNAVAILABLE",
			)
		}
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	mediaType := mime.TypeByExtension(filepath.Ext(request.PathValue("logicalName")))
	if content.Format == "RETROM_MULTIDISC_M3U_V1" {
		mediaType = "audio/x-mpegurl; charset=utf-8"
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	body, etag, err := launchGameBody(file, stat.Size(), content)
	if err != nil {
		if isMultiDisc {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
				content.ArtifactVersion, content.DiscCount, "PLAYLIST", http.StatusServiceUnavailable, 0,
				"CAS_UNAVAILABLE",
			)
		}
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	writer.Header().Set("ETag", `"sha256-`+etag+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	metricsWriter := &multiDiscResponseWriter{ResponseWriter: writer}
	http.ServeContent(metricsWriter, request, request.PathValue("logicalName"), time.Unix(0, 0), body)
	if isMultiDisc {
		status := metricsWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		resultCode := "OK"
		if status >= http.StatusBadRequest {
			resultCode = "HTTP_ERROR"
		}
		logMultiDiscContentResponse(
			request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreID,
			content.ArtifactVersion, content.DiscCount, "PLAYLIST", status, metricsWriter.bytes, resultCode,
		)
	}
}

func (server *Server) runtimeContent(request *http.Request) (launch.ContentView, error) {
	launchID := request.PathValue("launchId")
	capability := server.launchCapability(request)
	logicalName := request.PathValue("logicalName")
	content, err := server.launcher.Content(request.Context(), launchID, capability, logicalName)
	if err == nil {
		return content, nil
	}
	content, err = server.launcher.ReviewPreviewContent(request.Context(), launchID, capability, logicalName)
	if err != nil {
		return launch.ContentView{}, fmt.Errorf("review preview content: %w", err)
	}
	return content, nil
}

func launchGameBody(file io.ReadSeeker, size int64, content launch.ContentView) (io.ReadSeeker, string, error) {
	if content.Format != "RETROM_DOS_DIRECT_ZIP_V1" {
		return file, content.Digest, nil
	}
	if content.CoreID != "dosbox_pure" {
		return nil, "", fmt.Errorf("launch game body: %w", dosbundle.ErrInvalid)
	}
	var overlay *dosbundle.Overlay
	var err error
	readerAt, ok := file.(io.ReaderAt)
	if !ok {
		return nil, "", fmt.Errorf("launch game reader: %w", dosbundle.ErrInvalid)
	}
	if content.DOSEntry == nil {
		overlay, err = dosbundle.NewMenu(readerAt, size)
	} else {
		overlay, err = dosbundle.New(readerAt, size, *content.DOSEntry)
	}
	if err != nil {
		return nil, "", fmt.Errorf("build DOS overlay: %w", err)
	}
	digestInput := content.Format + "\x00" + content.Digest + "\x00" + nullableDOSEntry(content.DOSEntry)
	digest := sha256.Sum256([]byte(digestInput))
	return overlay, hex.EncodeToString(digest[:]), nil
}

func nullableDOSEntry(entry *string) string {
	if entry == nil {
		return "<menu>"
	}
	return *entry
}

func (server *Server) launchExternalFile(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	content, err := server.launcher.External(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		request.PathValue("logicalName"),
	)
	if err != nil {
		content, err = server.launcher.ReviewPreviewExternal(
			request.Context(), request.PathValue("launchId"), server.launchCapability(request),
			request.PathValue("logicalName"),
		)
	}
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动外部文件不可用", map[string]any{})
		return
	}
	file, err := server.blobs.OpenDigest(content.Digest)
	if err != nil {
		if content.Kind == "DISC" {
			logMultiDiscContentResponse(
				request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreKey,
				content.ArtifactVersion, content.DiscCount, "DISC", http.StatusUnauthorized, 0,
				"LAUNCH_CREDENTIAL_INVALID",
			)
		}
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动外部文件不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+content.Digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	metricsWriter := &multiDiscResponseWriter{ResponseWriter: writer}
	http.ServeContent(metricsWriter, request, request.PathValue("logicalName"), time.Unix(0, 0), file)
	if content.Kind == "DISC" {
		status := metricsWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		resultCode := "OK"
		if status >= http.StatusBadRequest {
			resultCode = "HTTP_ERROR"
		}
		logMultiDiscContentResponse(
			request.Context(), request.PathValue("launchId"), content.PlatformKey, content.CoreKey,
			content.ArtifactVersion, content.DiscCount, "DISC", status, metricsWriter.bytes, resultCode,
		)
	}
}

func (server *Server) launchBIOSBundle(writer http.ResponseWriter, request *http.Request) {
	server.launchBundle(writer, request, "BIOS_BUNDLE")
}

func (server *Server) launchParentBundle(writer http.ResponseWriter, request *http.Request) {
	server.launchBundle(writer, request, "PARENT")
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (server *Server) launchBundle(writer http.ResponseWriter, request *http.Request, kind string) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	files, err := server.launcher.BundleFiles(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		kind,
	)
	if err != nil {
		files, err = server.launcher.ReviewPreviewBundleFiles(
			request.Context(), request.PathValue("launchId"), server.launchCapability(request), kind,
		)
	}
	if errors.Is(err, launch.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if len(files) == 0 {
		writeError(writer, request, http.StatusNotFound, "LAUNCH_CONTENT_NOT_FOUND", "启动依赖不存在", map[string]any{})
		return
	}
	temporary, err := os.CreateTemp(filepath.Join(server.config.DataDir, "tmp", "jobs"), ".launch-bundle-")
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法装配启动依赖", map[string]any{})
		return
	}
	temporaryPath := temporary.Name()
	defer cleanup.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法装配启动依赖", map[string]any{})
		return
	}
	archiveWriter := zip.NewWriter(temporary)
	for _, entry := range files {
		if entry.LogicalName == "" || filepath.Base(entry.LogicalName) != entry.LogicalName ||
			strings.Contains(entry.LogicalName, "\\") {
			cleanup.Error("close", archiveWriter.Close())
			cleanup.Error("close", temporary.Close())
			writeError(
				writer,
				request,
				http.StatusServiceUnavailable,
				"LAUNCH_DEPENDENCY_INVALID",
				"启动依赖清单无效",
				map[string]any{},
			)
			return
		}
		header := deterministicStoreZIPHeader(entry.LogicalName)
		destination, createErr := archiveWriter.CreateHeader(header)
		if createErr != nil {
			cleanup.Error("close", archiveWriter.Close())
			cleanup.Error("close", temporary.Close())
			writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法装配启动依赖", map[string]any{})
			return
		}
		source, openErr := server.blobs.OpenDigest(entry.SHA256)
		if openErr != nil {
			cleanup.Error("close", archiveWriter.Close())
			cleanup.Error("close", temporary.Close())
			writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "启动依赖不可用", map[string]any{})
			return
		}
		_, copyErr := io.Copy(destination, source)
		cleanup.Error("close", source.Close())
		if copyErr != nil {
			cleanup.Error("close", archiveWriter.Close())
			cleanup.Error("close", temporary.Close())
			writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法读取启动依赖", map[string]any{})
			return
		}
	}
	if err := archiveWriter.Close(); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法完成启动依赖", map[string]any{})
		return
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法读取启动依赖", map[string]any{})
		return
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, temporary); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法校验启动依赖", map[string]any{})
		return
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", temporary.Close())
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "无法读取启动依赖", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", temporary.Close()) }()
	writer.Header().Set("Content-Type", "application/zip")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+hex.EncodeToString(digest.Sum(nil))+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "bundle.zip", time.Unix(0, 0), temporary)
}

func (server *Server) launchStart(writer http.ResponseWriter, request *http.Request) {
	server.recordPlay(writer, request, "start")
}

func (server *Server) launchHeartbeat(writer http.ResponseWriter, request *http.Request) {
	server.recordPlay(writer, request, "heartbeat")
}

func (server *Server) launchFinish(writer http.ResponseWriter, request *http.Request) {
	server.recordPlay(writer, request, "finish")
}

func (server *Server) recordPlay(writer http.ResponseWriter, request *http.Request, kind string) {
	var body launch.PlayEvent
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "游玩事件无效", map[string]any{})
		return
	}
	result, err := server.launcher.RecordPlay(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		kind,
		body,
	)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "PLAY_SEQUENCE_GAP", "游玩事件序号或会话状态无效", map[string]any{})
		return
	}
	if kind == "finish" {
		http.SetCookie(
			writer,
			&http.Cookie{
				Name:     "retrom_launch_" + request.PathValue("launchId"),
				Value:    "",
				Path:     "/runtime/launches/" + request.PathValue("launchId") + "/",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   server.config.PublicOrigin.Scheme == "https",
			},
		)
	}
	writeJSON(writer, http.StatusOK, result)
}

func validIdempotencyKey(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == strings.ToLower(value) && (parsed.Version() == 4 || parsed.Version() == 7)
}

func (server *Server) createSaveState(writer http.ResponseWriter, request *http.Request) {
	if server.rejectNetplaySave(writer, request) {
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	if request.ContentLength > (75 << 20) {
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"SAVE_STATE_TOO_LARGE",
			"存档内容超过限制",
			map[string]any{},
		)
		return
	}
	result, replayed, err := server.saveService.CreateManual(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		key,
		request,
	)
	switch {
	case errors.Is(err, saves.ErrCredential):
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
	case errors.Is(err, saves.ErrTooLarge):
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"SAVE_STATE_TOO_LARGE",
			"存档内容超过限制",
			map[string]any{},
		)
	case errors.Is(err, saves.ErrSequenceReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
	case err != nil:
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "存档请求无效", map[string]any{})
	default:
		if replayed {
			writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
		}
		writeJSON(writer, http.StatusCreated, result)
	}
}

func (server *Server) getPersistentSave(writer http.ResponseWriter, request *http.Request) {
	if server.rejectNetplaySave(writer, request) {
		return
	}
	metadata, exists, err := server.saveService.GetPersistent(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
	)
	if errors.Is(err, saves.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if errors.Is(err, saves.ErrPersistentUnsupported) {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"PERSISTENT_SAVE_UNSUPPORTED",
			"当前核心不支持自动持久存档",
			map[string]any{},
		)
		return
	}
	if errors.Is(err, saves.ErrTooLarge) {
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"LAUNCH_PERSISTENT_SAVE_TOO_LARGE",
			"持久存档超过限制",
			map[string]any{},
		)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if !exists {
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Set("Vary", "Cookie")
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	server.serveBlob(writer, request, metadata.SHA256, "application/octet-stream", true)
}

func (server *Server) putPersistentSave(writer http.ResponseWriter, request *http.Request) {
	if server.rejectNetplaySave(writer, request) {
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	if request.ContentLength > 64<<20 {
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"LAUNCH_PERSISTENT_SAVE_TOO_LARGE",
			"持久存档超过限制",
			map[string]any{},
		)
		return
	}
	sequence, err := strconv.ParseInt(request.Header.Get("X-Retrom-Save-Sequence"), 10, 64)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "存档序号无效", map[string]any{})
		return
	}
	result, replayed, err := server.saveService.PutPersistent(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		key,
		request.Header.Get("Content-Digest"),
		request.Header.Get("X-Retrom-Save-Event"),
		sequence,
		request.Body,
	)
	switch {
	case errors.Is(err, saves.ErrCredential):
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
	case errors.Is(err, saves.ErrPersistentUnsupported):
		writeError(
			writer,
			request,
			http.StatusConflict,
			"PERSISTENT_SAVE_UNSUPPORTED",
			"当前核心不支持自动持久存档",
			map[string]any{},
		)
	case errors.Is(err, saves.ErrTooLarge):
		writeError(
			writer,
			request,
			http.StatusRequestEntityTooLarge,
			"LAUNCH_PERSISTENT_SAVE_TOO_LARGE",
			"持久存档超过限制",
			map[string]any{},
		)
	case errors.Is(err, saves.ErrIdempotencyReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
	case errors.Is(err, saves.ErrSequenceGap):
		writeError(writer, request, http.StatusConflict, "SAVE_SEQUENCE_GAP", "存档序号不连续", map[string]any{})
	case errors.Is(err, saves.ErrSequenceReused):
		writeError(writer, request, http.StatusConflict, "SAVE_SEQUENCE_REUSED", "存档序号已用于不同内容", map[string]any{})
	case errors.Is(err, saves.ErrPersistentConflict):
		writeError(writer, request, http.StatusConflict, "PERSISTENT_SAVE_CONFLICT", "服务器存档已由另一会话更新", map[string]any{})
	case err != nil:
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "持久存档请求无效", map[string]any{})
	default:
		if replayed {
			writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
		}
		writeJSON(writer, http.StatusCreated, result)
	}
}

func (server *Server) rejectNetplaySave(writer http.ResponseWriter, request *http.Request) bool {
	access, err := server.launcher.SaveAccess(
		request.Context(), request.PathValue("launchId"), server.launchCapability(request),
	)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return true
	}
	if access == "NETPLAY_DISABLED" {
		writeError(writer, request, http.StatusConflict, "NETPLAY_SAVE_UNSUPPORTED", "联机模式不支持存档", map[string]any{})
		return true
	}
	return false
}

func (server *Server) launchState(writer http.ResponseWriter, request *http.Request) {
	if server.rejectNetplaySave(writer, request) {
		return
	}
	if rejectMultipleRanges(writer, request) {
		return
	}
	digest, err := server.saveService.StateDigest(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
	)
	if errors.Is(err, saves.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "LAUNCH_CONTENT_NOT_FOUND", "启动内容不存在", map[string]any{})
		return
	}
	server.serveBlob(writer, request, digest, "application/octet-stream", true)
}

func (server *Server) serveBlob(
	writer http.ResponseWriter,
	request *http.Request,
	digest, mediaType string,
	private bool,
) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	if private {
		writer.Header().Set("Cache-Control", "private, no-store")
		writer.Header().Set("Vary", "Cookie")
	} else {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("ETag", `"sha256-`+digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, "content", time.Unix(0, 0), file)
}

func rejectMultipleRanges(writer http.ResponseWriter, request *http.Request) bool {
	if strings.Contains(request.Header.Get("Range"), ",") {
		writeError(
			writer,
			request,
			http.StatusRequestedRangeNotSatisfiable,
			"MULTIPLE_RANGES_UNSUPPORTED",
			"一次只能请求一个字节范围",
			map[string]any{},
		)
		return true
	}
	return false
}

func deterministicStoreZIPHeader(name string) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	// archive/zip otherwise emits an extended-timestamp extra field. These DOS
	// fields are the only public API for the exact empty-Extra 1980 header.
	header.ModifiedDate = 33 //nolint:staticcheck // Required by RETROM_EJS_DEP_ZIP_V1.
	header.ModifiedTime = 0  //nolint:staticcheck // Required by RETROM_EJS_DEP_ZIP_V1.
	return header
}

type recentGameProjection struct {
	GameID           string              `json:"gameId"`
	Title            string              `json:"title"`
	Platform         map[string]any      `json:"platform"`
	PlatformInstance map[string]any      `json:"platformInstance"`
	LastPlayedAtMS   int64               `json:"lastPlayedAtMs"`
	ActiveDurationMS int64               `json:"activeDurationMs"`
	SessionCount     int64               `json:"sessionCount"`
	CoverURL         any                 `json:"coverUrl"`
	Tags             []tagging.Reference `json:"tags"`
}

type latestGameProjection struct {
	GameID           string              `json:"gameId"`
	Title            string              `json:"title"`
	Platform         map[string]any      `json:"platform"`
	PlatformInstance map[string]any      `json:"platformInstance"`
	CreatedAtMS      int64               `json:"createdAtMs"`
	CoverURL         any                 `json:"coverUrl"`
	Tags             []tagging.Reference `json:"tags"`
}

func scanRecentGame(scanner rowScanner) (recentGameProjection, error) {
	var game recentGameProjection
	var platformID, platformName, instanceID, instanceName string
	var coverAssetID sql.NullString
	if err := scanner.Scan(&game.GameID, &game.Title, &platformID, &platformName, &instanceID, &instanceName,
		&game.LastPlayedAtMS, &game.ActiveDurationMS, &game.SessionCount, &coverAssetID); err != nil {
		return recentGameProjection{}, fmt.Errorf("scan recent game: %w", err)
	}
	game.Platform = map[string]any{"id": platformID, "name": platformName}
	game.PlatformInstance = map[string]any{"id": instanceID, "name": instanceName}
	game.CoverURL = gameCoverURL(coverAssetID)
	return game, nil
}

func scanLatestGame(scanner rowScanner) (latestGameProjection, error) {
	var game latestGameProjection
	var platformID, platformName, instanceID, instanceName string
	var coverAssetID sql.NullString
	if err := scanner.Scan(&game.GameID, &game.Title, &platformID, &platformName, &instanceID, &instanceName,
		&game.CreatedAtMS, &coverAssetID); err != nil {
		return latestGameProjection{}, fmt.Errorf("scan latest game: %w", err)
	}
	game.Platform = map[string]any{"id": platformID, "name": platformName}
	game.PlatformInstance = map[string]any{"id": instanceID, "name": instanceName}
	game.CoverURL = gameCoverURL(coverAssetID)
	return game, nil
}

func (server *Server) projectRecentGameTags(ctx context.Context, games []recentGameProjection) error {
	gameIDs := make([]string, 0, len(games))
	for _, game := range games {
		gameIDs = append(gameIDs, game.GameID)
	}
	references, err := server.tagService.References(ctx, gameIDs)
	if err != nil {
		return fmt.Errorf("project recent game tags: %w", err)
	}
	for index := range games {
		games[index].Tags = references[games[index].GameID]
		if games[index].Tags == nil {
			games[index].Tags = []tagging.Reference{}
		}
	}
	return nil
}

func (server *Server) projectLatestGameTags(ctx context.Context, games []latestGameProjection) error {
	gameIDs := make([]string, 0, len(games))
	for _, game := range games {
		gameIDs = append(gameIDs, game.GameID)
	}
	references, err := server.tagService.References(ctx, gameIDs)
	if err != nil {
		return fmt.Errorf("project latest game tags: %w", err)
	}
	for index := range games {
		games[index].Tags = references[games[index].GameID]
		if games[index].Tags == nil {
			games[index].Tags = []tagging.Reference{}
		}
	}
	return nil
}

type tagReferenceLoader func(context.Context, []string) (map[string][]tagging.Reference, error)

func projectMapTags(
	ctx context.Context,
	items []map[string]any,
	idKey string,
	load tagReferenceLoader,
) error {
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		itemID, ok := item[idKey].(string)
		if !ok {
			return fmt.Errorf("%w: %s", errTagProjectionType, idKey)
		}
		itemIDs = append(itemIDs, itemID)
	}
	references, err := load(ctx, itemIDs)
	if err != nil {
		return fmt.Errorf("load tag projection for %s: %w", idKey, err)
	}
	for _, item := range items {
		itemID, ok := item[idKey].(string)
		if !ok {
			return fmt.Errorf("%w after loading: %s", errTagProjectionType, idKey)
		}
		item["tags"] = references[itemID]
		if item["tags"] == nil {
			item["tags"] = []tagging.Reference{}
		}
	}
	return nil
}

//nolint:funlen // The dashboard aggregates documented counters in one consistent response snapshot.
func (server *Server) home(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	var gameCount, saveCount, reviewCount, activeDurationMS int64
	if err := server.database.QueryRowContext(
		request.Context(),
		`SELECT count(*)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1`,
	).Scan(
		&gameCount,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.database.QueryRowContext(
		request.Context(),
		`SELECT count(*)
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE s.deleted_at_ms IS NULL
AND s.profile_id=?
AND g.status='PUBLISHED'
AND pi.enabled=1`,
		principal.ProfileID,
	).Scan(
		&saveCount,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.database.QueryRowContext(
		request.Context(),
		"SELECT count(*) FROM import_items WHERE state = 'REVIEW_PENDING'",
	).Scan(
		&reviewCount,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.database.QueryRowContext(
		request.Context(),
		`SELECT COALESCE(sum(ps.active_duration_ms),0)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
AND ps.profile_id=?`,
		principal.ProfileID,
	).Scan(
		&activeDurationMS,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	recentGames, err := server.homeRecentGames(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	recentSaves := make([]map[string]any, 0, 3)
	saveRows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT s.id,
s.game_id,
m.title,
s.name,
s.created_at_ms,
s.active_duration_ms,
s.disc_index
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE s.deleted_at_ms IS NULL
AND s.profile_id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
ORDER BY s.created_at_ms DESC,
s.id DESC LIMIT 3
`,
		principal.ProfileID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", saveRows.Close()) }()
	for saveRows.Next() {
		var saveID, gameID, title, name string
		var createdAtMS, activeDurationMS int64
		var discIndex sql.NullInt64
		if err := saveRows.Scan(
			&saveID, &gameID, &title, &name, &createdAtMS, &activeDurationMS, &discIndex,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		recentSaves = append(
			recentSaves,
			map[string]any{
				"saveStateId":      saveID,
				"gameId":           gameID,
				"gameTitle":        title,
				"name":             name,
				"createdAtMs":      createdAtMS,
				"activeDurationMs": activeDurationMS,
				"discIndex":        nullableInteger(discIndex),
				"discLabel":        discLabel(discIndex),
				"screenshotUrl":    saveStateScreenshotURL(saveID),
			},
		)
	}
	if err := saveRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := projectMapTags(
		request.Context(), recentSaves, "gameId", server.tagService.References,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	latestGames, err := server.homeLatestGames(request.Context())
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	featuredGame, err := server.homeFeaturedGame(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	platforms, quickPlatforms, err := server.homePlatforms(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"library":        map[string]any{"gameCount": gameCount, "saveStateCount": saveCount},
		"imports":        map[string]any{"reviewPendingCount": reviewCount},
		"play":           map[string]any{"activeDurationMs": activeDurationMS},
		"featuredGame":   featuredGame.Value,
		"latestGames":    latestGames,
		"recentGames":    recentGames,
		"recentSaves":    recentSaves,
		"platforms":      platforms,
		"quickPlatforms": quickPlatforms,
	})
}

func (server *Server) homeRecentGames(ctx context.Context, profileID string) ([]recentGameProjection, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
max(ps.started_at_ms),
sum(ps.active_duration_ms),
count(ps.id),
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
AND ps.profile_id=?
GROUP BY g.id,m.title,p.id,p.name,pi.id,pi.name
ORDER BY max(ps.started_at_ms) DESC,g.id DESC
LIMIT 10
`, profileID)
	if err != nil {
		return nil, fmt.Errorf("home recent games: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	games := make([]recentGameProjection, 0, 10)
	for rows.Next() {
		game, scanErr := scanRecentGame(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("home recent game rows: %w", err)
	}
	if err := server.projectRecentGameTags(ctx, games); err != nil {
		return nil, err
	}
	return games, nil
}

func (server *Server) homeLatestGames(ctx context.Context) ([]latestGameProjection, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
g.created_at_ms,
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
ORDER BY g.created_at_ms DESC,g.id DESC
LIMIT 10
`)
	if err != nil {
		return nil, fmt.Errorf("home latest games: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	games := make([]latestGameProjection, 0, 10)
	for rows.Next() {
		game, scanErr := scanLatestGame(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("home latest game rows: %w", err)
	}
	if err := server.projectLatestGameTags(ctx, games); err != nil {
		return nil, err
	}
	return games, nil
}

type homeFeaturedResult struct {
	Value map[string]any
}

type homeSessionSave struct {
	value any
}

func (server *Server) featuredSessionSave(
	ctx context.Context,
	launchID, profileID string,
) (homeSessionSave, error) {
	var saveID string
	var createdAtMS, activeDurationMS int64
	var discIndex sql.NullInt64
	err := server.database.QueryRowContext(ctx, `
SELECT id,created_at_ms,active_duration_ms,disc_index
FROM save_states
WHERE source_launch_session_id=? AND profile_id=? AND deleted_at_ms IS NULL
ORDER BY created_at_ms DESC,id DESC
LIMIT 1
	`, launchID, profileID).Scan(&saveID, &createdAtMS, &activeDurationMS, &discIndex)
	if errors.Is(err, sql.ErrNoRows) {
		return homeSessionSave{}, nil
	}
	if err != nil {
		return homeSessionSave{}, fmt.Errorf("home featured session save: %w", err)
	}
	return homeSessionSave{value: map[string]any{
		"saveStateId": saveID, "createdAtMs": createdAtMS, "activeDurationMs": activeDurationMS,
		"discIndex": nullableInteger(discIndex), "discLabel": discLabel(discIndex),
		"screenshotUrl": saveStateScreenshotURL(saveID),
	}}, nil
}

func (server *Server) activeGameTags(ctx context.Context, gameID string) ([]tagging.Reference, error) {
	references, err := server.tagService.References(ctx, []string{gameID})
	if err != nil {
		return nil, fmt.Errorf("project game tags: %w", err)
	}
	tags := references[gameID]
	if tags == nil {
		tags = []tagging.Reference{}
	}
	return tags, nil
}

func (server *Server) gameAssociations(
	ctx context.Context,
	profileID, gameID string,
) (*favorites.FavoriteReference, []tagging.Reference, error) {
	favorite, err := server.favoriteService.Reference(ctx, profileID, gameID)
	if err != nil {
		return nil, nil, fmt.Errorf("project game favorite: %w", err)
	}
	tags, err := server.activeGameTags(ctx, gameID)
	if err != nil {
		return nil, nil, err
	}
	return favorite, tags, nil
}

func (server *Server) homeFeaturedGame(ctx context.Context, profileID string) (homeFeaturedResult, error) {
	var launchID, gameID, title, platformID, platformName, instanceID, instanceName string
	var lastPlayedAtMS, activeDurationMS, sessionCount int64
	var coverAssetID sql.NullString
	err := server.database.QueryRowContext(ctx, `
SELECT ps.launch_session_id,
g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
ps.started_at_ms,
(SELECT COALESCE(sum(all_sessions.active_duration_ms),0)
 FROM play_sessions all_sessions
 WHERE all_sessions.game_id=g.id AND all_sessions.profile_id=?),
(SELECT count(*) FROM play_sessions all_sessions WHERE all_sessions.game_id=g.id AND all_sessions.profile_id=?),
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
AND ps.profile_id=?
ORDER BY ps.started_at_ms DESC,ps.id DESC
LIMIT 1
`, profileID, profileID, profileID).Scan(
		&launchID, &gameID, &title, &platformID, &platformName, &instanceID, &instanceName,
		&lastPlayedAtMS, &activeDurationMS, &sessionCount, &coverAssetID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return homeFeaturedResult{}, nil
	}
	if err != nil {
		return homeFeaturedResult{}, fmt.Errorf("home featured game: %w", err)
	}
	var historicalSaveCount int64
	if err := server.database.QueryRowContext(ctx, `
SELECT count(*) FROM save_states WHERE game_id=? AND profile_id=? AND deleted_at_ms IS NULL
`, gameID, profileID).Scan(&historicalSaveCount); err != nil {
		return homeFeaturedResult{}, fmt.Errorf("home featured save count: %w", err)
	}
	lastSessionSave, err := server.featuredSessionSave(ctx, launchID, profileID)
	if err != nil {
		return homeFeaturedResult{}, err
	}
	tags, err := server.activeGameTags(ctx, gameID)
	if err != nil {
		return homeFeaturedResult{}, err
	}
	return homeFeaturedResult{Value: map[string]any{
		"gameId": gameID, "title": title,
		"platform":         map[string]any{"id": platformID, "name": platformName},
		"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
		"lastPlayedAtMs":   lastPlayedAtMS, "activeDurationMs": activeDurationMS,
		"sessionCount": sessionCount, "coverUrl": gameCoverURL(coverAssetID),
		"hasSaveStates": historicalSaveCount > 0, "lastSessionSave": lastSessionSave.value, "tags": tags,
	}}, nil
}

type homePlatform struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GameCount int64  `json:"gameCount"`
	PlayCount int64  `json:"playCount"`
}

func (server *Server) homePlatforms(ctx context.Context, profileID string) ([]homePlatform, []homePlatform, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT p.id,p.name,count(DISTINCT g.id),count(ps.id)
FROM platforms p
LEFT JOIN platform_instances pi ON pi.platform_id=p.id AND pi.enabled=1 AND pi.deleted_at_ms IS NULL
LEFT JOIN games g ON g.platform_instance_id=pi.id AND g.status='PUBLISHED'
LEFT JOIN play_sessions ps ON ps.game_id=g.id AND ps.profile_id=?
WHERE EXISTS (SELECT 1 FROM platform_cores pc WHERE pc.platform_id=p.id AND pc.enabled=1)
GROUP BY p.id,p.name
ORDER BY p.name COLLATE NOCASE,p.id
`, profileID)
	if err != nil {
		return nil, nil, fmt.Errorf("home platforms: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	platforms := make([]homePlatform, 0, 8)
	for rows.Next() {
		var platform homePlatform
		if err := rows.Scan(&platform.ID, &platform.Name, &platform.GameCount, &platform.PlayCount); err != nil {
			return nil, nil, fmt.Errorf("home platform row: %w", err)
		}
		platforms = append(platforms, platform)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("home platform rows: %w", err)
	}
	quickPlatforms := append([]homePlatform(nil), platforms...)
	sort.Slice(quickPlatforms, func(left, right int) bool {
		if quickPlatforms[left].PlayCount != quickPlatforms[right].PlayCount {
			return quickPlatforms[left].PlayCount > quickPlatforms[right].PlayCount
		}
		if quickPlatforms[left].Name != quickPlatforms[right].Name {
			return quickPlatforms[left].Name < quickPlatforms[right].Name
		}
		return quickPlatforms[left].ID < quickPlatforms[right].ID
	})
	if len(quickPlatforms) > 4 {
		quickPlatforms = quickPlatforms[:4]
	}
	return platforms, quickPlatforms, nil
}

// recentGames returns every visible game with play history, ordered by the
// most recently started play session. This is a game projection rather than a
// session log, so one game always occupies one row regardless of play count.
func (server *Server) recentGames(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	rows, err := server.database.QueryContext(request.Context(), `
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
max(ps.started_at_ms),
sum(ps.active_duration_ms),
count(ps.id),
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
AND ps.profile_id=?
GROUP BY g.id,m.title,p.id,p.name,pi.id,pi.name
ORDER BY max(ps.started_at_ms) DESC,g.id DESC
`, principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]recentGameProjection, 0)
	for rows.Next() {
		game, err := scanRecentGame(rows)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, game)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.projectRecentGameTags(request.Context(), items); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"generatedAtMs": server.now().UnixMilli(), "items": items})
}

func (server *Server) games(writer http.ResponseWriter, request *http.Request) {
	server.gameList(writer, request, false)
}

//nolint:funlen,gocyclo // Method dispatch and nullable detail projections stay at the route protocol boundary.
func (server *Server) game(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	gameID := request.PathValue("gameId")
	var title, description, developer, publisher, genre string
	var platformID, platformName, instanceID, instanceName, contentRevisionID string
	var players, releaseYear sql.NullInt64
	var coverAssetID, videoAssetID sql.NullString
	var version, updatedAtMS, activeDurationMS int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT m.title,
m.description,
m.developer,
m.publisher,
m.genre,
m.players,
m.release_year,
p.id,
p.name,
pi.id,
pi.name,
g.current_content_revision_id,
g.version,
g.updated_at_ms,
(SELECT a.id
FROM game_assets a
WHERE a.game_id=g.id
AND a.metadata_revision_id=g.current_metadata_revision_id
AND a.kind='COVER'
ORDER BY a.ordinal,
a.id
LIMIT 1),
(SELECT a.id
FROM game_assets a
WHERE a.game_id=g.id
AND a.metadata_revision_id=g.current_metadata_revision_id
AND a.kind='VIDEO'
AND a.ordinal=0
ORDER BY a.id
LIMIT 1),
COALESCE((SELECT SUM(active_duration_ms)
FROM play_sessions ps
WHERE ps.game_id=g.id
AND ps.profile_id=?),
0)
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
`, principal.ProfileID, gameID).
		Scan(&title, &description, &developer, &publisher, &genre, &players, &releaseYear,
			&platformID, &platformName, &instanceID, &instanceName, &contentRevisionID,
			&version, &updatedAtMS, &coverAssetID, &videoAssetID, &activeDurationMS,
		)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT c.id,
c.name,
c.requires_threads,
pi.default_core_id,
v.current_revision_id,
r.id,
r.core_artifact_id,
r.dat_version_id,
r.status,
r.compatibility_code
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platform_cores pc ON pc.platform_id=pi.platform_id
AND pc.enabled=1
JOIN cores c ON c.id=pc.core_id
AND c.enabled=1
LEFT
JOIN game_variants v ON v.game_id=g.id
AND v.core_id=c.id
LEFT
JOIN game_variant_revisions r ON r.id=v.current_revision_id
AND r.game_content_revision_id=g.current_content_revision_id
WHERE g.id=?
ORDER BY c.name,
c.id
`,
		gameID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	coreOptions := make([]map[string]any, 0)
	for rows.Next() {
		var coreID, coreName, defaultCoreID string
		var requiresThreads int
		var currentRevision, revisionID, artifactID, datVersionID, status, compatibility sql.NullString
		if err := rows.Scan(
			&coreID,
			&coreName,
			&requiresThreads,
			&defaultCoreID,
			&currentRevision,
			&revisionID,
			&artifactID,
			&datVersionID,
			&status,
			&compatibility,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		projectedStatus := "NEEDS_VALIDATION"
		var reasons []map[string]any
		switch {
		case revisionID.Valid && status.String == "READY":
			projectedStatus = "READY"
			reasons = []map[string]any{}
		case revisionID.Valid && status.String == "BLOCKED":
			projectedStatus = "DEPENDENCY_MISSING"
			reasons = []map[string]any{{"code": compatibility.String, "level": "BLOCKING"}}
		case revisionID.Valid:
			projectedStatus = "INCOMPATIBLE"
			reasons = []map[string]any{{"code": compatibility.String, "level": "BLOCKING"}}
		default:
			reasons = []map[string]any{{"code": "VARIANT_VALIDATION_REQUIRED", "level": "INFO"}}
		}
		coreOptions = append(coreOptions, map[string]any{
			"coreId": coreID, "name": coreName, "isDefault": coreID == defaultCoreID, "status": projectedStatus,
			"revalidationStatus": "NOT_REQUIRED", "currentVariantRevisionId": nullableString(currentRevision),
			"coreArtifactId": nullableString(
				artifactID,
			), "datVersionId": nullableString(datVersionID), "revalidationJobId": nil,
			"requiresThreads": requiresThreads == 1, "reasons": reasons,
		})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	dosEntries := make([]map[string]any, 0)
	dosRows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe
FROM dos_entries
WHERE game_content_revision_id=?
ORDER BY rank,
normalized_path
`,
		contentRevisionID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", dosRows.Close()) }()
	for dosRows.Next() {
		var normalizedPath, originalPath, kind string
		var rank int64
		var enabled, directLaunchSafe int
		if err := dosRows.Scan(&normalizedPath, &originalPath, &kind, &rank, &enabled, &directLaunchSafe); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		dosEntries = append(dosEntries, map[string]any{
			"path": normalizedPath, "originalPath": originalPath, "kind": kind, "rank": rank,
			"enabled": enabled == 1, "directLaunchSafe": directLaunchSafe == 1,
		})
	}
	if err := dosRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var defaultDOSEntry sql.NullString
	err = server.database.QueryRowContext(request.Context(), `
SELECT r.default_dos_entry
FROM games g
JOIN game_variants v ON v.game_id=g.id
AND v.core_id='dosbox_pure'
JOIN game_variant_revisions r ON r.id=v.current_revision_id
AND r.game_content_revision_id=g.current_content_revision_id
WHERE g.id=?
`, gameID).Scan(&defaultDOSEntry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		server.databaseError(writer, request, err)
		return
	}
	var saveStateCount int64
	if err := server.database.QueryRowContext(request.Context(), `
SELECT count(*)
FROM save_states
WHERE game_id=?
AND profile_id=?
AND deleted_at_ms IS NULL
`, gameID, principal.ProfileID).Scan(&saveStateCount); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	saveRows, err := server.database.QueryContext(request.Context(), `
SELECT s.id,
s.name,
s.created_at_ms,
a.core_id,
c.name,
s.disc_index
FROM save_states s
JOIN core_artifacts a ON a.id=s.core_artifact_id
JOIN cores c ON c.id=a.core_id
WHERE s.game_id=?
AND s.profile_id=?
AND s.deleted_at_ms IS NULL
ORDER BY s.created_at_ms DESC,
s.id DESC
LIMIT 8
`, gameID, principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", saveRows.Close()) }()
	saveStates := make([]map[string]any, 0)
	for saveRows.Next() {
		var saveID, saveName, coreID, coreName string
		var createdAtMS int64
		var discIndex sql.NullInt64
		if err := saveRows.Scan(&saveID, &saveName, &createdAtMS, &coreID, &coreName, &discIndex); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		saveStates = append(saveStates, map[string]any{
			"saveStateId": saveID, "name": saveName, "createdAtMs": createdAtMS,
			"discIndex": nullableInteger(discIndex), "discLabel": discLabel(discIndex),
			"screenshotUrl": saveStateScreenshotURL(saveID),
			"core":          map[string]any{"id": coreID, "name": coreName},
		})
	}
	if err := saveRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	favorite, tags, err := server.gameAssociations(request.Context(), principal.ProfileID, gameID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"gameId": gameID, "title": title, "description": description, "developer": developer, "publisher": publisher,
		"genre": genre, "players": nullableInteger(players), "releaseYear": nullableInteger(releaseYear),
		"platform":                 map[string]any{"id": platformID, "name": platformName},
		"platformInstance":         map[string]any{"id": instanceID, "name": instanceName},
		"currentContentRevisionId": contentRevisionID, "version": version, "updatedAtMs": updatedAtMS,
		"coverUrl": gameCoverURL(
			coverAssetID,
		), "videoUrl": gameCoverURL(videoAssetID), "activeDurationMs": activeDurationMS, "coreOptions": coreOptions,
		"dosEntries": dosEntries, "defaultDosEntry": nullableString(defaultDOSEntry),
		"saveStateCount": saveStateCount, "saveStates": saveStates,
		"favorite": favorite, "tags": tags,
	})
}

func (server *Server) adminGames(writer http.ResponseWriter, request *http.Request) {
	server.gameList(writer, request, true)
}

func gameListVisibilityConditions(includeDisabled bool) []string {
	if includeDisabled {
		return nil
	}
	return []string{"pi.enabled=1"}
}

type gameListFilters struct {
	Conditions  []string
	Arguments   []any
	NormalizedQ string
}

type gameListFacet struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PlatformID string `json:"platformId,omitempty"`
	Count      int64  `json:"count"`
}

type gameListFacets struct {
	TotalCount        int64           `json:"totalCount"`
	Platforms         []gameListFacet `json:"platforms"`
	PlatformInstances []gameListFacet `json:"platformInstances"`
	Tags              []gameListFacet `json:"tags"`
}

func queryGameListFacetRows(
	ctx context.Context,
	database *sql.DB,
	query string,
	suffix string,
	includePlatform bool,
) ([]gameListFacet, error) {
	visible := []string{"g.status='PUBLISHED'", "pi.enabled=1"}
	rows, err := database.QueryContext(ctx, queryWithConditions(query, visible, suffix))
	if err != nil {
		return nil, fmt.Errorf("query game facets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]gameListFacet, 0)
	for rows.Next() {
		var item gameListFacet
		if includePlatform {
			err = rows.Scan(&item.ID, &item.Name, &item.PlatformID, &item.Count)
		} else {
			err = rows.Scan(&item.ID, &item.Name, &item.Count)
		}
		if err != nil {
			return nil, fmt.Errorf("scan game facet: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game facets: %w", err)
	}
	return items, nil
}

func queryGameListFacets(
	ctx context.Context,
	database *sql.DB,
	filteredConditions []string,
	filteredArguments []any,
) (int64, gameListFacets, error) {
	baseFrom := `
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
`
	var filteredCount int64
	if err := database.QueryRowContext(
		ctx,
		queryWithConditions("SELECT count(*) "+baseFrom, filteredConditions, ""),
		filteredArguments...,
	).Scan(&filteredCount); err != nil {
		return 0, gameListFacets{}, fmt.Errorf("count filtered games: %w", err)
	}

	platforms, err := queryGameListFacetRows(
		ctx,
		database,
		"SELECT p.id,p.name,count(*) "+baseFrom,
		" GROUP BY p.id,p.name ORDER BY p.name,p.id",
		false,
	)
	if err != nil {
		return 0, gameListFacets{}, fmt.Errorf("list game platform facets: %w", err)
	}
	platformInstances, err := queryGameListFacetRows(
		ctx,
		database,
		"SELECT pi.id,pi.name,p.id,count(*) "+baseFrom,
		" GROUP BY pi.id,pi.name,p.id ORDER BY pi.name,pi.id",
		true,
	)
	if err != nil {
		return 0, gameListFacets{}, fmt.Errorf("list game directory facets: %w", err)
	}
	tagFrom := baseFrom + `
JOIN game_tags relation ON relation.game_id=g.id
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
`
	tags, err := queryGameListFacetRows(
		ctx,
		database,
		"SELECT tag.id,tag.name,count(*) "+tagFrom,
		" GROUP BY tag.id,tag.name ORDER BY tag.name,tag.id",
		false,
	)
	if err != nil {
		return 0, gameListFacets{}, fmt.Errorf("list game tag facets: %w", err)
	}
	facets := gameListFacets{Platforms: platforms, PlatformInstances: platformInstances, Tags: tags}
	for _, platform := range platforms {
		facets.TotalCount += platform.Count
	}
	return filteredCount, facets, nil
}

func parseGameListFilters(values url.Values, includeDeleted bool) (gameListFilters, error) {
	filters := gameListFilters{Conditions: gameListVisibilityConditions(includeDeleted)}
	status := values.Get("status")
	switch {
	case !includeDeleted || status == "PUBLISHED":
		filters.Conditions = append(filters.Conditions, "g.status='PUBLISHED'")
	case status == "DELETED":
		filters.Conditions = append(filters.Conditions, "g.status='DELETED'")
	case status != "" && status != "ALL":
		return gameListFilters{}, fmt.Errorf("%w: game status", errUnknownQuery)
	}
	filters.NormalizedQ = strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " "))
	if filters.NormalizedQ != "" {
		filters.Conditions = append(filters.Conditions, `(instr(g.search_text,?)>0 OR EXISTS(
SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id=g.id AND instr(tag.search_text,?)>0))`)
		filters.Arguments = append(filters.Arguments, filters.NormalizedQ, filters.NormalizedQ)
	}
	if tagID := values.Get("tagId"); tagID != "" {
		if !tagging.ValidID(tagID) {
			return gameListFilters{}, errInvalidGameTagFilter
		}
		filters.Conditions = append(filters.Conditions, `EXISTS(
SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id=g.id AND tag.id=?)`)
		filters.Arguments = append(filters.Arguments, tagID)
	}
	for _, filter := range []struct{ queryName, column string }{
		{"platformId", "p.id"}, {"platformInstanceId", "pi.id"},
	} {
		if value := values.Get(filter.queryName); value != "" {
			filters.Conditions = append(filters.Conditions, filter.column+"=?")
			filters.Arguments = append(filters.Arguments, value)
		}
	}
	return filters, nil
}

func writeGameListFilterError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, errInvalidGameTagFilter) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "标签筛选无效", map[string]any{})
		return
	}
	writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏状态筛选无效", map[string]any{})
}

func scanGameListItem(scanner rowScanner, includeAdminProjection bool) (map[string]any, error) {
	var id, title, platformID, platformName, instanceID, instanceName, defaultCoreID, defaultCoreName, status string
	var version, createdAtMS, updatedAtMS int64
	var lastPlayedAtMS, releaseYear sql.NullInt64
	var metadataComplete int64
	var runtimeStatus, coverAssetID sql.NullString
	if err := scanner.Scan(
		&id,
		&title,
		&platformID,
		&platformName,
		&instanceID,
		&instanceName,
		&defaultCoreID,
		&defaultCoreName,
		&status,
		&version,
		&createdAtMS,
		&updatedAtMS,
		&lastPlayedAtMS,
		&releaseYear,
		&metadataComplete,
		&runtimeStatus,
		&coverAssetID,
	); err != nil {
		return nil, fmt.Errorf("scan game list item: %w", err)
	}
	item := map[string]any{
		"gameId": id, "title": title, "platform": map[string]any{"id": platformID, "name": platformName},
		"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
		"defaultCore":      map[string]any{"id": defaultCoreID, "name": defaultCoreName},
		"status":           status, "version": version, "createdAtMs": createdAtMS, "updatedAtMs": updatedAtMS,
		"lastPlayedAtMs": nullableInteger(lastPlayedAtMS), "coverUrl": gameCoverURL(coverAssetID),
	}
	if includeAdminProjection {
		item["releaseYear"] = nullableInteger(releaseYear)
		item["metadataComplete"] = metadataComplete == 1
		item["runtimeStatus"] = nullableString(runtimeStatus)
	}
	return item, nil
}

func (server *Server) projectGameListFavorites(
	ctx context.Context,
	profileID string,
	items []map[string]any,
) error {
	gameIDs := make([]string, 0, len(items))
	for _, item := range items {
		gameID, _ := item["gameId"].(string)
		gameIDs = append(gameIDs, gameID)
	}
	references, err := server.favoriteService.References(ctx, profileID, gameIDs)
	if err != nil {
		return fmt.Errorf("project game list favorites: %w", err)
	}
	for _, item := range items {
		gameID, _ := item["gameId"].(string)
		if favorite, exists := references[gameID]; exists {
			item["favorite"] = favorite
		} else {
			item["favorite"] = nil
		}
	}
	return nil
}

func (server *Server) projectGameListTags(ctx context.Context, items []map[string]any) error {
	return projectMapTags(ctx, items, "gameId", server.tagService.References)
}

func scanGameListRows(rows *sql.Rows, includeDeleted bool, capacity int) ([]map[string]any, error) {
	items := make([]map[string]any, 0, capacity)
	for rows.Next() {
		item, err := scanGameListItem(rows, includeDeleted)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game list: %w", err)
	}
	return items, nil
}

func gameListSortCode(raw string, includeDeleted bool) (string, error) {
	if raw == "" {
		if includeDeleted {
			return "UPDATED_DESC", nil
		}
		return "RECENT_DESC", nil
	}
	switch raw {
	case "TITLE_ASC", "ADDED_DESC":
		return raw, nil
	case "RECENT_DESC":
		if !includeDeleted {
			return raw, nil
		}
	case "UPDATED_DESC":
		if includeDeleted {
			return raw, nil
		}
	}
	return "", errUnknownQuery
}

func gameListInteger(item map[string]any, key string, fallback int64) int64 {
	value, ok := item[key].(int64)
	if !ok {
		return fallback
	}
	return value
}

func appendGameListTitleCursor(payload cursor.Payload, conditions *[]string, arguments *[]any) error {
	if len(payload.SortValues) != 1 {
		return errInvalidCursorPayload
	}
	*conditions = append(*conditions, "(m.title>? OR (m.title=? AND g.id>?))")
	*arguments = append(*arguments, payload.SortValues[0], payload.SortValues[0], payload.ID)
	return nil
}

func appendGameListTimestampCursor(
	payload cursor.Payload,
	column string,
	conditions *[]string,
	arguments *[]any,
) error {
	if len(payload.SortValues) != 2 {
		return errInvalidCursorPayload
	}
	timestamp, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
	if err != nil {
		return errInvalidCursorPayload
	}
	*conditions = append(*conditions, fmt.Sprintf(
		"(%s<? OR (%s=? AND (m.title>? OR (m.title=? AND g.id>?))))", column, column,
	))
	*arguments = append(*arguments, timestamp, timestamp, payload.SortValues[1], payload.SortValues[1], payload.ID)
	return nil
}

func appendGameListRecentCursor(
	payload cursor.Payload,
	profileID string,
	conditions *[]string,
	arguments *[]any,
) error {
	if len(payload.SortValues) != 3 {
		return errInvalidCursorPayload
	}
	lastPlayed, playedErr := strconv.ParseInt(payload.SortValues[0], 10, 64)
	createdAt, createdErr := strconv.ParseInt(payload.SortValues[1], 10, 64)
	if playedErr != nil || createdErr != nil {
		return errInvalidCursorPayload
	}
	lastPlayedExpression := `COALESCE((SELECT max(ps_cursor.started_at_ms)
FROM play_sessions ps_cursor WHERE ps_cursor.game_id=g.id AND ps_cursor.profile_id=?),-1)`
	*conditions = append(*conditions, fmt.Sprintf(
		`(%s<? OR (%s=? AND (g.created_at_ms<? OR (g.created_at_ms=? AND (m.title>? OR (m.title=? AND g.id>?))))))`,
		lastPlayedExpression, lastPlayedExpression,
	))
	*arguments = append(*arguments,
		profileID, lastPlayed, profileID, lastPlayed,
		createdAt, createdAt, payload.SortValues[2], payload.SortValues[2], payload.ID,
	)
	return nil
}

func (server *Server) applyGameListCursor(
	token string,
	operationID string,
	filterDigest string,
	sortCode string,
	profileID string,
	conditions *[]string,
	arguments *[]any,
) error {
	if token == "" {
		return nil
	}
	payload, err := server.cursors.Decode(token, operationID, filterDigest, sortCode)
	if err != nil {
		return errInvalidCursorPayload
	}
	switch sortCode {
	case "TITLE_ASC":
		return appendGameListTitleCursor(payload, conditions, arguments)
	case "ADDED_DESC":
		return appendGameListTimestampCursor(payload, "g.created_at_ms", conditions, arguments)
	case "UPDATED_DESC":
		return appendGameListTimestampCursor(payload, "g.updated_at_ms", conditions, arguments)
	case "RECENT_DESC":
		return appendGameListRecentCursor(payload, profileID, conditions, arguments)
	default:
		return errInvalidCursorPayload
	}
}

func gameListCursorSortValues(item map[string]any, sortCode, title string) []string {
	switch sortCode {
	case "RECENT_DESC":
		return []string{
			strconv.FormatInt(gameListInteger(item, "lastPlayedAtMs", -1), 10),
			strconv.FormatInt(gameListInteger(item, "createdAtMs", 0), 10),
			title,
		}
	case "ADDED_DESC":
		return []string{strconv.FormatInt(gameListInteger(item, "createdAtMs", 0), 10), title}
	case "UPDATED_DESC":
		return []string{strconv.FormatInt(gameListInteger(item, "updatedAtMs", 0), 10), title}
	default:
		return []string{title}
	}
}

func (server *Server) encodeGameListNextCursor(
	items []map[string]any,
	limit int,
	operationID string,
	filterDigest string,
	sortCode string,
) ([]map[string]any, any, error) {
	if len(items) <= limit {
		return items, nil, nil
	}
	last := items[limit-1]
	lastTitle, titleOK := last["title"].(string)
	lastID, idOK := last["gameId"].(string)
	if !titleOK || !idOK {
		return nil, nil, errGamePagination
	}
	token, err := server.cursors.Encode(cursor.Payload{
		OperationID:  operationID,
		FilterDigest: filterDigest,
		SortCode:     sortCode,
		SortValues:   gameListCursorSortValues(last, sortCode, lastTitle),
		ID:           lastID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("encode game cursor: %w", err)
	}
	return items[:limit], token, nil
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (server *Server) gameList(writer http.ResponseWriter, request *http.Request, includeDeleted bool) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	query := `
SELECT g.id,
 m.title,
 p.id,
 p.name,
 pi.id,
 pi.name,
 dc.id,
 dc.name,
 g.status,
 g.version,
 g.created_at_ms,
 g.updated_at_ms,
 (SELECT max(ps.started_at_ms) FROM play_sessions ps WHERE ps.game_id=g.id AND ps.profile_id=?) AS last_played_at_ms,
 m.release_year,
 CASE WHEN trim(m.description)<>''
 AND trim(m.developer)<>''
 AND trim(m.publisher)<>''
 AND trim(m.genre)<>''
 AND m.players IS NOT NULL
 AND m.release_year IS NOT NULL THEN 1 ELSE 0 END,
 (SELECT vr.status
 FROM game_variants v
 JOIN game_variant_revisions vr ON vr.id=v.current_revision_id
 WHERE v.game_id=g.id
 AND v.core_id=pi.default_core_id
 LIMIT 1),
 (SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,
 a.id
 LIMIT 1)
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
JOIN cores dc ON dc.id=pi.default_core_id
`
	values := request.URL.Query()
	filters, err := parseGameListFilters(values, includeDeleted)
	if err != nil {
		writeGameListFilterError(writer, request, err)
		return
	}
	conditions := filters.Conditions
	arguments := append([]any{principal.ProfileID}, filters.Arguments...)
	baseConditions := append([]string(nil), filters.Conditions...)
	baseArguments := append([]any(nil), filters.Arguments...)
	normalizedQ := filters.NormalizedQ
	sortCode, err := gameListSortCode(values.Get("sort"), includeDeleted)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏排序无效", map[string]any{})
		return
	}
	operationID := "getGames"
	if includeDeleted {
		operationID = "getAdminGames"
	}
	filterDigest := cursor.FilterDigest(
		map[string]any{
			"principalId":        principal.UserID,
			"q":                  normalizedQ,
			"tagId":              values.Get("tagId"),
			"platformId":         values.Get("platformId"),
			"platformInstanceId": values.Get("platformInstanceId"),
			"status":             values.Get("status"),
			"sort":               sortCode,
		},
	)
	if err := server.applyGameListCursor(
		values.Get("cursor"), operationID, filterDigest, sortCode, principal.ProfileID, &conditions, &arguments,
	); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	order := " ORDER BY m.title ASC,g.id ASC LIMIT ?"
	switch sortCode {
	case "RECENT_DESC":
		order = " ORDER BY last_played_at_ms DESC,g.created_at_ms DESC,m.title ASC,g.id ASC LIMIT ?"
	case "ADDED_DESC":
		order = " ORDER BY g.created_at_ms DESC,m.title ASC,g.id ASC LIMIT ?"
	case "UPDATED_DESC":
		order = " ORDER BY g.updated_at_ms DESC,m.title ASC,g.id ASC LIMIT ?"
	}
	query = queryWithConditions(query, conditions, order)
	arguments = append(arguments, limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items, err := scanGameListRows(rows, includeDeleted, limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.projectGameListTags(request.Context(), items); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if !includeDeleted {
		if err := server.projectGameListFavorites(request.Context(), principal.ProfileID, items); err != nil {
			server.databaseError(writer, request, err)
			return
		}
	}
	items, nextCursor, err := server.encodeGameListNextCursor(items, limit, operationID, filterDigest, sortCode)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	response := map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor,
	}
	if !includeDeleted && values.Get("cursor") == "" {
		filteredCount, facets, facetErr := queryGameListFacets(
			request.Context(), server.database, baseConditions, baseArguments,
		)
		if facetErr != nil {
			server.databaseError(writer, request, facetErr)
			return
		}
		response["filteredCount"] = filteredCount
		response["facets"] = facets
	}
	writeJSON(writer, http.StatusOK, response)
}

type saveListFilters struct {
	Conditions   []string
	Arguments    []any
	NormalizedQ  string
	Availability string
	Digest       string
}

func parseSaveListFilters(values url.Values, principal authn.Principal) (saveListFilters, error) {
	filters := saveListFilters{
		Conditions:   []string{"s.profile_id=?", "s.deleted_at_ms IS NULL", "pi.enabled=1"},
		Arguments:    []any{principal.ProfileID},
		NormalizedQ:  strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " ")),
		Availability: values.Get("availability"),
	}
	if filters.NormalizedQ != "" {
		filters.Conditions = append(filters.Conditions, "(instr(g.search_text,?)>0 OR instr(lower(s.name),?)>0)")
		filters.Arguments = append(filters.Arguments, filters.NormalizedQ, filters.NormalizedQ)
	}
	for _, filter := range []struct{ queryName, column string }{
		{"gameId", "s.game_id"},
		{"platformId", "pi.platform_id"},
		{"platformInstanceId", "pi.id"},
		{"coreId", "a.core_id"},
	} {
		if value := values.Get(filter.queryName); value != "" {
			filters.Conditions = append(filters.Conditions, filter.column+"=?")
			filters.Arguments = append(filters.Arguments, value)
		}
	}
	if filters.Availability == "" {
		filters.Availability = "AVAILABLE"
	}
	switch filters.Availability {
	case "AVAILABLE":
		filters.Conditions = append(filters.Conditions, "g.status='PUBLISHED'")
	case "BLOCKED":
		filters.Conditions = append(filters.Conditions, "g.status!='PUBLISHED'")
	case "ALL":
	default:
		return saveListFilters{}, fmt.Errorf("%w: availability", errUnknownQuery)
	}
	filters.Digest = cursor.FilterDigest(map[string]any{
		"principalId":        principal.UserID,
		"q":                  filters.NormalizedQ,
		"gameId":             values.Get("gameId"),
		"platformId":         values.Get("platformId"),
		"platformInstanceId": values.Get("platformInstanceId"),
		"coreId":             values.Get("coreId"),
		"availability":       filters.Availability,
	})
	return filters, nil
}

func (server *Server) applySaveCursor(values url.Values, filters *saveListFilters) error {
	token := values.Get("cursor")
	if token == "" {
		return nil
	}
	payload, err := server.cursors.Decode(token, "getSaves", filters.Digest, "CREATED_DESC")
	if err != nil || len(payload.SortValues) != 1 {
		return errInvalidCursorPayload
	}
	createdAt, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
	if err != nil {
		return errInvalidCursorPayload
	}
	filters.Conditions = append(filters.Conditions, "(s.created_at_ms<? OR (s.created_at_ms=? AND s.id<?))")
	filters.Arguments = append(filters.Arguments, createdAt, createdAt, payload.ID)
	return nil
}

//nolint:funlen // Query projection stays contiguous with pagination assembly.
func (server *Server) saves(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	values := request.URL.Query()
	filters, err := parseSaveListFilters(values, principal)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "存档可用性筛选无效", map[string]any{})
		return
	}
	if err := server.applySaveCursor(values, &filters); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	query := queryWithConditions(
		`
SELECT s.id,
s.game_id,
m.title,
s.name,
s.version,
s.created_at_ms,
s.active_duration_ms,
a.core_id,
c.name,
g.status,
p.id,
p.name,
pi.id,
pi.name,
s.disc_index
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN core_artifacts a ON a.id=s.core_artifact_id
JOIN cores c ON c.id=a.core_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
`,
		filters.Conditions,
		` ORDER BY s.created_at_ms DESC,s.id DESC LIMIT ?`,
	)
	filters.Arguments = append(filters.Arguments, limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, filters.Arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0, limit+1)
	for rows.Next() {
		var id, gameID, gameTitle, name, coreID, coreName, gameStatus string
		var platformID, platformName, instanceID, instanceName string
		var version, createdAtMS, activeDurationMS int64
		var discIndex sql.NullInt64
		if err := rows.Scan(
			&id,
			&gameID,
			&gameTitle,
			&name,
			&version,
			&createdAtMS,
			&activeDurationMS,
			&coreID,
			&coreName,
			&gameStatus,
			&platformID,
			&platformName,
			&instanceID,
			&instanceName,
			&discIndex,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"saveStateId": id, "gameId": gameID, "gameTitle": gameTitle,
			"name": name, "version": version, "createdAtMs": createdAtMS,
			"discIndex": nullableInteger(discIndex), "discLabel": discLabel(discIndex),
			"activeDurationMs": activeDurationMS, "screenshotUrl": saveStateScreenshotURL(id),
			"core": map[string]any{
				"id":   coreID,
				"name": coreName,
			}, "platformId": platformID, "platform": map[string]any{"id": platformID, "name": platformName},
			"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
			"availability": map[string]any{
				"status":  map[bool]string{true: "AVAILABLE", false: "BLOCKED"}[gameStatus == "PUBLISHED"],
				"reasons": []any{},
			},
		})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := projectMapTags(request.Context(), items, "gameId", server.tagService.References); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		createdAtMS, createdOK := last["createdAtMs"].(int64)
		lastID, idOK := last["saveStateId"].(string)
		if !createdOK || !idOK {
			writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "存档分页投影无效", map[string]any{})
			return
		}
		token, err := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getSaves",
				FilterDigest: filters.Digest,
				SortCode:     "CREATED_DESC",
				SortValues:   []string{strconv.FormatInt(createdAtMS, 10)},
				ID:           lastID,
			},
		)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		nextCursor = token
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor,
	})
}

func (server *Server) patchSave(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil || strings.TrimSpace(body.Name) != body.Name ||
		body.Name == "" ||
		len([]rune(body.Name)) > 120 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "存档名称无效", map[string]any{})
		return
	}
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前资源版本",
			map[string]any{},
		)
		return
	}
	now := server.now().UnixMilli()
	result, err := server.database.ExecContext(
		request.Context(),
		`
UPDATE save_states
SET name=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND profile_id=?
AND version=?
AND deleted_at_ms IS NULL
`,
		body.Name,
		now,
		request.PathValue("saveStateId"),
		principal.ProfileID,
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		var exists int
		lookupErr := server.database.QueryRowContext(request.Context(), `
SELECT 1 FROM save_states WHERE id=? AND profile_id=? AND deleted_at_ms IS NULL
`, request.PathValue("saveStateId"), principal.ProfileID).Scan(&exists)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			writeError(writer, request, http.StatusNotFound, "SAVE_STATE_NOT_FOUND", "存档不存在", map[string]any{})
			return
		}
		if lookupErr != nil {
			server.databaseError(writer, request, lookupErr)
			return
		}
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "存档已被修改", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"saveStateId": request.PathValue("saveStateId"),
			"name":        body.Name,
			"version":     expected + 1,
			"updatedAtMs": now,
		},
	)
}

func (server *Server) deleteSave(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前资源版本",
			map[string]any{},
		)
		return
	}
	now := server.now().UnixMilli()
	result, err := server.database.ExecContext(
		request.Context(),
		`
UPDATE save_states
SET deleted_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND profile_id=?
AND version=?
AND deleted_at_ms IS NULL
`,
		now,
		now,
		request.PathValue("saveStateId"),
		principal.ProfileID,
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		var exists int
		lookupErr := server.database.QueryRowContext(request.Context(), `
SELECT 1 FROM save_states WHERE id=? AND profile_id=? AND deleted_at_ms IS NULL
`, request.PathValue("saveStateId"), principal.ProfileID).Scan(&exists)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			writeError(writer, request, http.StatusNotFound, "SAVE_STATE_NOT_FOUND", "存档不存在", map[string]any{})
			return
		}
		if lookupErr != nil {
			server.databaseError(writer, request, lookupErr)
			return
		}
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "存档已被修改", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) platforms(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT p.id,
p.name,
p.sort_order,
p.enabled,
pc.core_id,
c.name,
pc.enabled
FROM platforms p
LEFT JOIN platform_cores pc ON pc.platform_id=p.id
LEFT JOIN cores c ON c.id=pc.core_id
ORDER BY p.sort_order,
pc.core_id
`,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	byID := make(map[string]map[string]any)
	for rows.Next() {
		var id, name string
		var sortOrder, enabled int
		var coreID, coreName sql.NullString
		var coreEnabled sql.NullInt64
		if err := rows.Scan(&id, &name, &sortOrder, &enabled, &coreID, &coreName, &coreEnabled); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		item := byID[id]
		if item == nil {
			item = map[string]any{
				"id":        id,
				"name":      name,
				"sortOrder": sortOrder,
				"enabled":   enabled == 1,
				"cores":     []map[string]any{},
			}
			byID[id] = item
			items = append(items, item)
		}
		if coreID.Valid {
			cores, ok := item["cores"].([]map[string]any)
			if !ok {
				writeError(
					writer,
					request,
					http.StatusInternalServerError,
					"INTERNAL_ERROR",
					"平台核心投影无效",
					map[string]any{},
				)
				return
			}
			item["cores"] = append(
				cores,
				map[string]any{"id": coreID.String, "name": coreName.String, "enabled": coreEnabled.Int64 == 1},
			)
		}
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (server *Server) coreArtifacts(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT a.id,
a.core_id,
c.name,
a.emulatorjs_version,
a.bundle_version,
a.flavor,
a.enabled,
a.version,
a.size_bytes
FROM core_artifacts a
JOIN cores c ON c.id=a.core_id
ORDER BY c.name,
a.emulatorjs_version,
a.id
`,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, coreID, coreName, ejsVersion, bundleVersion, flavor string
		var enabled int
		var version, size int64
		if err := rows.Scan(
			&id,
			&coreID,
			&coreName,
			&ejsVersion,
			&bundleVersion,
			&flavor,
			&enabled,
			&version,
			&size,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(
			items,
			map[string]any{
				"id":                id,
				"coreId":            coreID,
				"coreName":          coreName,
				"emulatorjsVersion": ejsVersion,
				"bundleVersion":     bundleVersion,
				"flavor":            flavor,
				"enabled":           enabled == 1,
				"version":           version,
				"sizeBytes":         size,
			},
		)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func platformInstanceFilters(values url.Values) ([]string, []any, bool) {
	conditions := []string{"pi.deleted_at_ms IS NULL"}
	arguments := make([]any, 0, 2)
	if value := values.Get("platformId"); value != "" {
		conditions = append(conditions, "pi.platform_id=?")
		arguments = append(arguments, value)
	}
	if value := values.Get("enabled"); value != "" {
		if value != "true" && value != "false" {
			return nil, nil, false
		}
		conditions = append(conditions, "pi.enabled=?")
		arguments = append(arguments, map[string]int{"true": 1, "false": 0}[value])
	}
	return conditions, arguments, true
}

func (server *Server) platformInstances(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	conditions, arguments, ok := platformInstanceFilters(values)
	if !ok {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "目录启用状态无效", map[string]any{})
		return
	}
	query := queryWithConditions(
		`
SELECT pi.id,
pi.platform_id,
p.name,
pi.default_core_id,
c.name,
pi.name,
pi.slug,
pi.description,
pi.sort_order,
pi.enabled,
pi.version,
pi.updated_at_ms,
(SELECT count(*) FROM games g WHERE g.platform_instance_id=pi.id)
,
COALESCE((SELECT a.compatibility_config_json
 FROM core_artifacts a
 WHERE a.core_id=pi.default_core_id
 AND a.enabled=1
 LIMIT 1),'{}')
FROM platform_instances pi
JOIN platforms p ON p.id=pi.platform_id
JOIN cores c ON c.id=pi.default_core_id
`,
		conditions,
		` ORDER BY pi.sort_order,pi.id LIMIT 100`,
	)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, platformID, platformName, coreID, coreName, name, slug, description, compatibility string
		var sortOrder, enabled int
		var version, updatedAtMS, gameCount int64
		if err := rows.Scan(
			&id,
			&platformID,
			&platformName,
			&coreID,
			&coreName,
			&name,
			&slug,
			&description,
			&sortOrder,
			&enabled,
			&version,
			&updatedAtMS,
			&gameCount,
			&compatibility,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "platformId": platformID, "platformName": platformName, "defaultCoreId": coreID,
			"defaultCoreName": coreName, "name": name, "slug": slug, "description": description,
			"sortOrder": sortOrder, "enabled": enabled == 1, "version": version, "updatedAtMs": updatedAtMS,
			"gameCount": gameCount, "supportedExtensions": contentprofile.SupportedExtensions(platformID),
			"importCapabilities": contentcapability.Resolve(
				platformID, enabled == 1, server.config.MultiDiscImportEnabled, compatibility,
			),
		})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

//nolint:funlen // DAT status and parse statistics are projected together as one versioned administrative resource.
func (server *Server) arcadeDATs(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	conditions := []string{"1=1"}
	arguments := make([]any, 0, 5)
	if value := strings.TrimSpace(values.Get("q")); value != "" {
		conditions = append(conditions, "(instr(lower(c.name),lower(?))>0 OR instr(lower(d.core_id),lower(?))>0)")
		arguments = append(arguments, value, value)
	}
	filters := map[string]string{
		"coreId": "d.core_id", "coreArtifactId": "d.core_artifact_id",
		"source": "d.source", "parseStatus": "d.parse_status",
	}
	for name, column := range filters {
		if value := values.Get(name); value != "" {
			conditions = append(conditions, column+"=?")
			arguments = append(arguments, value)
		}
	}
	query := queryWithConditions(
		`
SELECT d.id,
d.core_id,
c.name,
d.core_artifact_id,
d.source,
d.compatibility_status,
d.parse_status,
d.is_active,
d.machine_count,
d.rom_entry_count,
d.disk_entry_count,
d.bios_set_count,
d.version,
d.updated_at_ms,
j.id,
j.state,
j.version,
s.id,
s.state,
s.error_code,
s.version
FROM dat_versions d
JOIN cores c ON c.id=d.core_id
LEFT JOIN dat_import_jobs dj ON dj.dat_version_id=d.id
LEFT JOIN jobs j ON j.id=dj.job_id
LEFT JOIN dat_diff_snapshots s ON s.dat_version_id=d.id
`,
		conditions,
		` ORDER BY c.name,d.created_at_ms DESC,d.id LIMIT 100`,
	)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, coreID, coreName, artifactID, source, compatibility, status string
		var active int
		var machineCount, romCount, diskCount, biosCount sql.NullInt64
		var version, updatedAtMS int64
		var jobID, jobState sql.NullString
		var jobVersion sql.NullInt64
		var diffJobID, diffState, diffError sql.NullString
		var diffVersion sql.NullInt64
		if err := rows.Scan(
			&id,
			&coreID,
			&coreName,
			&artifactID,
			&source,
			&compatibility,
			&status,
			&active,
			&machineCount,
			&romCount,
			&diskCount,
			&biosCount,
			&version,
			&updatedAtMS,
			&jobID,
			&jobState,
			&jobVersion,
			&diffJobID,
			&diffState,
			&diffError,
			&diffVersion,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		projectedDiffState := "NOT_RUN"
		switch {
		case active == 1:
			projectedDiffState = "NOT_APPLICABLE"
		case status != "READY":
			projectedDiffState = "NOT_READY"
		case diffState.Valid:
			projectedDiffState = diffState.String
		}
		items = append(items, map[string]any{
			"id": id, "coreId": coreID, "coreName": coreName, "coreArtifactId": artifactID, "source": source,
			"compatibilityStatus": compatibility, "parseStatus": status, "active": active == 1,
			"machineCount": nullableInteger(machineCount), "romEntryCount": nullableInteger(romCount),
			"diskEntryCount": nullableInteger(diskCount), "biosSetCount": nullableInteger(biosCount),
			"version": version, "updatedAtMs": updatedAtMS, "jobId": nullableString(jobID),
			"jobState": nullableString(jobState), "jobVersion": nullableInteger(jobVersion),
			"diffJobId": nullableString(diffJobID), "diffStatus": projectedDiffState,
			"diffErrorCode": nullableString(diffError), "diffVersion": nullableInteger(diffVersion),
		})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func nullableInteger(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func discLabel(value sql.NullInt64) any {
	if value.Valid {
		return fmt.Sprintf("光盘 %d", value.Int64+1)
	}
	return nil
}

func nullableBIOSInstallation(
	id, md5Value, sha1Value, sha256Value sql.NullString,
	validatedVersion, installedAt sql.NullInt64,
) any {
	if !id.Valid {
		return nil
	}
	return map[string]any{
		"id":                          id.String,
		"md5":                         md5Value.String,
		"sha1":                        sha1Value.String,
		"sha256":                      sha256Value.String,
		"validatedRequirementVersion": validatedVersion.Int64,
		"createdAtMs":                 installedAt.Int64,
	}
}

type importOverviewSummary struct {
	Running         int64 `json:"running"`
	ReviewPending   int64 `json:"reviewPending"`
	PublishedItems  int64 `json:"publishedItems"`
	Completed       int64 `json:"completed"`
	Failed          int64 `json:"failed"`
	OrdinaryFailed  int64 `json:"ordinaryFailed"`
	PegasusFailed   int64 `json:"pegasusFailed"`
	ProcessingItems int64 `json:"processingItems"`
	IssueItems      int64 `json:"issueItems"`
}

func (server *Server) importSummary(writer http.ResponseWriter, request *http.Request) {
	var summary importOverviewSummary
	err := server.database.QueryRowContext(request.Context(), importOverviewSummarySQL).Scan(
		&summary.Running,
		&summary.ReviewPending,
		&summary.PublishedItems,
		&summary.Completed,
		&summary.Failed,
		&summary.OrdinaryFailed,
		&summary.PegasusFailed,
		&summary.ProcessingItems,
		&summary.IssueItems,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, summary)
}

const userVisibleImportJobPredicate = `i.id NOT IN (
 SELECT pegasus_item.library_import_job_id FROM pegasus_import_items pegasus_item
 WHERE pegasus_item.library_import_job_id IS NOT NULL
)`

const importOverviewSummarySQL = `
WITH ordinary AS (
 SELECT i.state,i.total_item_count,i.failed_item_count,i.rejected_file_count,i.resolved_rejected_file_count
 FROM import_jobs i
 WHERE ` + userVisibleImportJobPredicate + `
)
SELECT
 (SELECT count(*) FROM ordinary WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))+
 (SELECT count(*) FROM pegasus_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')),
 (SELECT count(*) FROM import_items WHERE state='REVIEW_PENDING'),
 (SELECT count(*) FROM import_items WHERE state='PUBLISHED'),
 (SELECT count(*) FROM ordinary WHERE state='COMPLETED')+
 (SELECT count(*) FROM pegasus_imports WHERE state='COMPLETED'),
 (SELECT count(*) FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED'))+
 (SELECT count(*) FROM pegasus_imports WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 (SELECT count(*) FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 (SELECT count(*) FROM pegasus_imports WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 COALESCE((SELECT sum(total_item_count) FROM ordinary
  WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')),0)+
 COALESCE((SELECT sum(game_count) FROM pegasus_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')),0),
 COALESCE((SELECT sum(failed_item_count+CASE
   WHEN rejected_file_count>resolved_rejected_file_count
   THEN rejected_file_count-resolved_rejected_file_count ELSE 0 END)
  FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED')),0)+
 COALESCE((SELECT sum(blocked_item_count+failed_item_count) FROM pegasus_imports
  WHERE state IN ('PARTIAL_FAILURE','FAILED')),0)
`

type importListFilters struct {
	queryText   string
	state       string
	platformID  string
	digest      string
	sortCode    string
	cursorToken string
	limit       int
	sortField   string
}

func parseImportListFilters(values url.Values, principalID string) (importListFilters, error) {
	filters := importListFilters{
		queryText:   strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " ")),
		state:       values.Get("state"),
		platformID:  values.Get("platformInstanceId"),
		sortCode:    values.Get("sort"),
		cursorToken: values.Get("cursor"),
		limit:       20,
		sortField:   "updatedAtMs",
	}
	if len([]rune(filters.queryText)) > 200 {
		return importListFilters{}, errQueryTooLong
	}
	if filters.state != "" && !validImportListState(filters.state) {
		return importListFilters{}, errUnknownQuery
	}
	if filters.sortCode == "" {
		filters.sortCode = "UPDATED_DESC"
	}
	if filters.sortCode == "CREATED_DESC" {
		filters.sortField = "createdAtMs"
	} else if filters.sortCode != "UPDATED_DESC" {
		return importListFilters{}, errUnknownQuery
	}
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 20 {
			return importListFilters{}, errInvalidLimit
		}
		filters.limit = parsed
	}
	filters.digest = cursor.FilterDigest(map[string]any{
		"principalId": principalID, "q": filters.queryText, "state": filters.state,
		"platformInstanceId": filters.platformID,
	})
	return filters, nil
}

func validImportListState(state string) bool {
	switch state {
	case "QUEUED",
		"RUNNING",
		"REVIEW_PENDING",
		"PARTIAL_FAILURE",
		"COMPLETED",
		"CANCEL_REQUESTED",
		"CANCELLED",
		"FAILED":
		return true
	default:
		return false
	}
}

func (server *Server) importListArguments(filters importListFilters) ([]any, error) {
	cursorID := ""
	cursorValue := int64(0)
	if filters.cursorToken != "" {
		payload, err := server.cursors.Decode(
			filters.cursorToken, "getAdminImports", filters.digest, filters.sortCode,
		)
		if err != nil || len(payload.SortValues) != 1 {
			return nil, errInvalidCursorPayload
		}
		parsed, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
		if err != nil {
			return nil, errInvalidCursorPayload
		}
		cursorID, cursorValue = payload.ID, parsed
	}
	return []any{
		filters.queryText, filters.queryText, filters.queryText,
		filters.state, filters.state,
		filters.platformID, filters.platformID,
		cursorID,
		filters.sortCode, cursorValue, cursorValue, cursorID,
		filters.sortCode, cursorValue, cursorValue, cursorID,
		filters.sortCode,
		filters.limit + 1,
	}, nil
}

const importListSQL = `
SELECT i.id,
i.state,
pi.name,
i.metadata_provider,
coalesce(json_extract(i.config_snapshot_json,'$.contentMode'),'STANDARD'),
i.total_item_count,
i.review_pending_item_count,
i.failed_item_count,
i.rejected_file_count,
i.resolved_rejected_file_count,
i.already_imported_item_count,
i.already_imported_file_count,
i.version,
i.created_at_ms,
i.updated_at_ms
FROM import_jobs i
JOIN platform_instances pi ON pi.id=i.target_platform_instance_id
WHERE ` + userVisibleImportJobPredicate + `
AND (?='' OR instr(lower(i.id),lower(?))>0 OR instr(lower(pi.name),lower(?))>0)
AND (?='' OR i.state=?)
AND (?='' OR i.target_platform_instance_id=?)
AND (?='' OR
(?='UPDATED_DESC' AND (i.updated_at_ms<? OR (i.updated_at_ms=? AND i.id<?))) OR
(?='CREATED_DESC' AND (i.created_at_ms<? OR (i.created_at_ms=? AND i.id<?))))
ORDER BY CASE ? WHEN 'UPDATED_DESC' THEN i.updated_at_ms WHEN 'CREATED_DESC' THEN i.created_at_ms END DESC,
i.id DESC
LIMIT ?
`

type importListItem struct {
	ID                          string `json:"id"`
	State                       string `json:"state"`
	PlatformInstanceName        string `json:"platformInstanceName"`
	MetadataProvider            string `json:"metadataProvider"`
	ContentMode                 string `json:"contentMode"`
	TotalItemCount              int64  `json:"totalItemCount"`
	ReviewPendingItemCount      int64  `json:"reviewPendingItemCount"`
	FailedItemCount             int64  `json:"failedItemCount"`
	RejectedFileCount           int64  `json:"rejectedFileCount"`
	UnresolvedRejectedFileCount int64  `json:"unresolvedRejectedFileCount"`
	AlreadyImportedItemCount    int64  `json:"alreadyImportedItemCount"`
	AlreadyImportedFileCount    int64  `json:"alreadyImportedFileCount"`
	Version                     int64  `json:"version"`
	CreatedAtMS                 int64  `json:"createdAtMs"`
	UpdatedAtMS                 int64  `json:"updatedAtMs"`
}

func queryImportList(
	ctx context.Context,
	database *sql.DB,
	arguments []any,
	capacity int,
) ([]importListItem, error) {
	rows, err := database.QueryContext(ctx, importListSQL, arguments...)
	if err != nil {
		return nil, fmt.Errorf("httpapi: query imports: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]importListItem, 0, capacity)
	for rows.Next() {
		var item importListItem
		var resolvedRejected int64
		if err := rows.Scan(
			&item.ID,
			&item.State,
			&item.PlatformInstanceName,
			&item.MetadataProvider,
			&item.ContentMode,
			&item.TotalItemCount,
			&item.ReviewPendingItemCount,
			&item.FailedItemCount,
			&item.RejectedFileCount,
			&resolvedRejected,
			&item.AlreadyImportedItemCount,
			&item.AlreadyImportedFileCount,
			&item.Version,
			&item.CreatedAtMS,
			&item.UpdatedAtMS,
		); err != nil {
			return nil, fmt.Errorf("httpapi: scan import: %w", err)
		}
		item.UnresolvedRejectedFileCount = item.RejectedFileCount - resolvedRejected
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("httpapi: scan imports: %w", err)
	}
	return items, nil
}

func (server *Server) encodeImportListCursor(
	filters importListFilters,
	items []importListItem,
) ([]importListItem, any, error) {
	if len(items) <= filters.limit {
		return items, nil, nil
	}
	last := items[filters.limit-1]
	items = items[:filters.limit]
	sortValue := last.UpdatedAtMS
	if filters.sortField == "createdAtMs" {
		sortValue = last.CreatedAtMS
	}
	token, err := server.cursors.Encode(cursor.Payload{
		OperationID:  "getAdminImports",
		FilterDigest: filters.digest,
		SortCode:     filters.sortCode,
		SortValues:   []string{strconv.FormatInt(sortValue, 10)},
		ID:           last.ID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("httpapi: encode import cursor: %w", err)
	}
	return items, token, nil
}

func (server *Server) imports(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	filters, err := parseImportListFilters(request.URL.Query(), principal.UserID)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "导入任务筛选无效", map[string]any{})
		return
	}
	arguments, err := server.importListArguments(filters)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	items, err := queryImportList(request.Context(), server.database, arguments, filters.limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items, nextCursor, err := server.encodeImportListCursor(filters, items)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (server *Server) createImport(writer http.ResponseWriter, request *http.Request) {
	var body libraryimport.CreateRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "导入配置无效", map[string]any{})
		return
	}
	if body.TagIDs != nil {
		if _, err := tagging.ValidateIDs(body.TagIDs); err != nil {
			writeTagError(writer, request, err)
			return
		}
	}
	created, err := server.importer.Create(request.Context(), body)
	switch {
	case errors.Is(err, libraryimport.ErrMultiDiscModeUnavailable):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"MULTI_DISC_MODE_UNAVAILABLE", "目标目录不支持多盘导入", map[string]any{},
		)
		return
	case errors.Is(err, libraryimport.ErrMultiDiscPlaylistMissing):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"MULTI_DISC_PLAYLIST_MISSING", "所选目录中没有 M3U 播放列表", map[string]any{},
		)
		return
	case errors.Is(err, tagging.ErrReferenceInvalid), errors.Is(err, tagging.ErrAssignmentLimitExceeded):
		writeTagError(writer, request, err)
		return
	case err != nil:
		writeError(writer, request, http.StatusConflict, "IMPORT_INPUT_INVALID", "上传或目标目录不可用于导入", map[string]any{})
		return
	}
	writeJSON(writer, http.StatusAccepted, created)
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (server *Server) reviews(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	query := `
SELECT i.id,
d.version,
i.import_job_id,
json_extract(d.metadata_json,
'$.title'),
COALESCE(json_extract(i.source_manifest_json,
'$[0].logicalName'),
json_extract(i.source_manifest_json,
'$.files[0].logicalName'),
json_extract(d.metadata_json,
'$.title')),
pi.id,
pi.name,
v.status,
v.compatibility_code,
i.updated_at_ms,
(SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.import_item_id=i.id
AND r.state='COMPLETED'),
(SELECT COALESCE(sum(b.size_bytes),0)
 FROM import_item_source_snapshot_files source_file
 JOIN blobs b ON b.id=source_file.blob_id
 WHERE source_file.source_snapshot_id=d.effective_source_snapshot_id),
(SELECT b.md5
 FROM import_item_source_snapshot_files source_file
 JOIN blobs b ON b.id=source_file.blob_id
 WHERE source_file.source_snapshot_id=d.effective_source_snapshot_id
 ORDER BY CASE source_file.role WHEN 'CONTENT' THEN 0 WHEN 'DOS_SOURCE' THEN 1 ELSE 2 END,
 source_file.sort_order,
 source_file.logical_name
 LIMIT 1),
COALESCE(d.cover_uploaded_asset_id,(SELECT asset.id
 FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.import_item_id=i.id
 AND run.state='COMPLETED'
 AND asset.status='READY'
 AND asset.kind_hint='COVER'
 ORDER BY CASE WHEN asset.id=d.cover_candidate_asset_id THEN 0 ELSE 1 END,
 run.completed_at_ms DESC,
 asset.ordinal,
 asset.id
 LIMIT 1))
,pegasus.id,pegasus.import_id,pegasus_collection.name,
EXISTS(
 SELECT 1 FROM pegasus_import_item_assets pegasus_asset
 WHERE pegasus_asset.item_id=pegasus.id AND pegasus_asset.kind='COVER'
 AND pegasus_asset.state='COPIED' AND pegasus_asset.blob_id IS NOT NULL
)
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
	LEFT JOIN pegasus_import_items pegasus ON pegasus.library_import_item_id=i.id
	LEFT JOIN pegasus_import_collections pegasus_collection ON pegasus_collection.id=pegasus.collection_id
	LEFT
JOIN import_item_core_validations v ON v.id=COALESCE(d.selected_validation_id,
(SELECT candidate.id
FROM import_item_core_validations candidate
WHERE candidate.import_item_id=i.id
AND candidate.source_snapshot_id=d.effective_source_snapshot_id
AND candidate.target_platform_instance_id=d.target_platform_instance_id
ORDER BY candidate.created_at_ms DESC,
candidate.id DESC LIMIT 1))
WHERE i.state='REVIEW_PENDING'
AND (pegasus.id IS NULL OR pegasus.execution_state='REVIEW_PENDING')
`
	arguments := []any{}
	values := request.URL.Query()
	allowed := map[string]struct{}{
		"q":                  {},
		"tagId":              {},
		"importJobId":        {},
		"pegasusImportId":    {},
		"platformInstanceId": {},
		"blockerCode":        {},
		"sort":               {},
		"cursor":             {},
		"limit":              {},
	}
	for key := range values {
		if _, ok := allowed[key]; !ok {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "待审核筛选包含未知字段", map[string]any{})
			return
		}
	}
	if importJobID := values.Get("importJobId"); importJobID != "" {
		query += " AND i.import_job_id=?"
		arguments = append(arguments, importJobID)
	}
	if pegasusImportID := values.Get("pegasusImportId"); pegasusImportID != "" {
		query += " AND pegasus.import_id=?"
		arguments = append(arguments, pegasusImportID)
	}
	normalizedQ := strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " "))
	if len([]rune(normalizedQ)) > 200 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "待审核搜索词过长", map[string]any{})
		return
	}
	if normalizedQ != "" {
		query += ` AND (instr(i.search_text,?)>0 OR EXISTS(
 SELECT 1 FROM review_draft_tags relation
 JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND instr(tag.name_key,?)>0
))`
		arguments = append(arguments, normalizedQ)
		arguments = append(arguments, normalizedQ)
	}
	if tagID := values.Get("tagId"); tagID != "" {
		if !tagging.ValidID(tagID) {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "标签筛选无效", map[string]any{})
			return
		}
		query += ` AND EXISTS(
 SELECT 1 FROM review_draft_tags relation
 JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND tag.id=?
)`
		arguments = append(arguments, tagID)
	}
	if value := values.Get("platformInstanceId"); value != "" {
		query += " AND d.target_platform_instance_id=?"
		arguments = append(arguments, value)
	}
	if value := values.Get("blockerCode"); value != "" {
		query += " AND (v.compatibility_code=? OR (?='NEEDS_VALIDATION' AND v.id IS NULL))"
		arguments = append(arguments, value)
		arguments = append(arguments, value)
	}
	sortCode := values.Get("sort")
	if sortCode == "" {
		sortCode = "UPDATED_ASC"
	}
	if sortCode != "UPDATED_ASC" && sortCode != "UPDATED_DESC" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "待审核排序无效", map[string]any{})
		return
	}
	filterDigest := cursor.FilterDigest(
		map[string]any{
			"principalId":        principal.UserID,
			"q":                  normalizedQ,
			"tagId":              values.Get("tagId"),
			"importJobId":        values.Get("importJobId"),
			"pegasusImportId":    values.Get("pegasusImportId"),
			"platformInstanceId": values.Get("platformInstanceId"),
			"blockerCode":        values.Get("blockerCode"),
		},
	)
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminReviews", filterDigest, sortCode)
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		updatedAt, parseErr := strconv.ParseInt(payload.SortValues[0], 10, 64)
		if parseErr != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		if sortCode == "UPDATED_ASC" {
			query += " AND (d.updated_at_ms>? OR (d.updated_at_ms=? AND i.id>?))"
		} else {
			query += " AND (d.updated_at_ms<? OR (d.updated_at_ms=? AND i.id<?))"
		}
		arguments = append(arguments, updatedAt, updatedAt, payload.ID)
	}
	limit := 20
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 20 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "分页大小无效", map[string]any{})
			return
		}
		limit = parsed
	}
	if sortCode == "UPDATED_ASC" {
		query += " ORDER BY d.updated_at_ms ASC,i.id ASC LIMIT ?"
	} else {
		query += " ORDER BY d.updated_at_ms DESC,i.id DESC LIMIT ?"
	}
	arguments = append(arguments, limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0, limit+1)
	for rows.Next() {
		var itemID, importJobID, title, sourceName, platformID, platformName string
		var reviewVersion, updatedAtMS, candidateCount, sourceTotalSizeBytes int64
		var validationStatus, compatibilityCode, sourceMD5, coverAssetID sql.NullString
		var pegasusItemID, pegasusImportID, pegasusCollectionName sql.NullString
		var hasPegasusCover int
		if err := rows.Scan(
			&itemID,
			&reviewVersion,
			&importJobID,
			&title,
			&sourceName,
			&platformID,
			&platformName,
			&validationStatus,
			&compatibilityCode,
			&updatedAtMS,
			&candidateCount,
			&sourceTotalSizeBytes,
			&sourceMD5,
			&coverAssetID,
			&pegasusItemID,
			&pegasusImportID,
			&pegasusCollectionName,
			&hasPegasusCover,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		blockers := []string{}
		if validationStatus.String != "READY" && compatibilityCode.Valid {
			blockers = append(blockers, compatibilityCode.String)
		}
		status := validationStatus.String
		if status == "" {
			status = "NEEDS_VALIDATION"
		}
		coverURL := reviewAssetURL(coverAssetID)
		if coverURL == nil && pegasusItemID.Valid && hasPegasusCover == 1 {
			coverURL = "/api/v1/admin/review-assets/" + pegasusItemID.String + "?kind=COVER"
		}
		sourceKind := "STANDARD"
		if pegasusImportID.Valid {
			sourceKind = "PEGASUS"
		}
		items = append(
			items,
			map[string]any{
				"itemId":               itemID,
				"reviewVersion":        reviewVersion,
				"importJobId":          importJobID,
				"sourceDisplayName":    sourceName,
				"draftTitle":           title,
				"platformInstance":     map[string]any{"id": platformID, "name": platformName},
				"validationStatus":     status,
				"validationJobId":      nil,
				"blockerCodes":         blockers,
				"candidateCount":       candidateCount,
				"sourceTotalSizeBytes": sourceTotalSizeBytes,
				"sourceMd5":            nullableString(sourceMD5),
				"coverUrl":             coverURL,
				"sourceKind":           sourceKind,
				"sourceLabel":          nullableString(pegasusCollectionName),
				"pegasusImportId":      nullableString(pegasusImportID),
				"updatedAtMs":          updatedAtMS,
			},
		)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := projectMapTags(
		request.Context(), items, "itemId", server.tagService.ReviewReferences,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		updatedAtMS, updatedOK := last["updatedAtMs"].(int64)
		lastID, idOK := last["itemId"].(string)
		if !updatedOK || !idOK {
			writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "审核分页投影无效", map[string]any{})
			return
		}
		token, err := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminReviews",
				FilterDigest: filterDigest,
				SortCode:     sortCode,
				SortValues:   []string{strconv.FormatInt(updatedAtMS, 10)},
				ID:           lastID,
			},
		)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		nextCursor = token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func decodeOptionalJSON(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	var decoded any
	_ = json.Unmarshal([]byte(value.String), &decoded)
	return decoded
}

func (server *Server) activeReviewTags(ctx context.Context, itemID string) ([]tagging.Reference, error) {
	references, err := server.tagService.ReviewReferences(ctx, []string{itemID})
	if err != nil {
		return nil, fmt.Errorf("project review tags: %w", err)
	}
	tags := references[itemID]
	if tags == nil {
		tags = []tagging.Reference{}
	}
	return tags, nil
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (server *Server) review(writer http.ResponseWriter, request *http.Request) {
	var itemID, importJobID, metadata, platformID, platformName, sourceSnapshotID, sourceManifest string
	var sourceContentKind, currentArtifactCompatibility string
	var validationID, validationStatus, compatibilityCode, dependencySnapshot sql.NullString
	var selectedValidationID sql.NullString
	var validationGeneration sql.NullInt64
	var selectedCandidateID, coverID, uploadedCoverID, backgroundID, defaultDOSEntry sql.NullString
	var version, updatedAtMS int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT i.id,
i.import_job_id,
d.metadata_json,
d.version,
d.updated_at_ms,
pi.id,
pi.name,
current_artifact.compatibility_config_json,
v.id,
v.status,
v.compatibility_code,
v.dependency_snapshot_json,
d.selected_validation_id,
source_snapshot.id,
source_snapshot.source_manifest_json,
source_snapshot.content_kind,
v.prepublish_generation,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.cover_uploaded_asset_id,
d.background_candidate_asset_id,
d.default_dos_entry
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=d.effective_source_snapshot_id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
JOIN core_artifacts current_artifact ON current_artifact.core_id=pi.default_core_id
AND current_artifact.enabled=1
LEFT
JOIN import_item_core_validations v ON v.id=(
  SELECT candidate.id
FROM import_item_core_validations candidate
WHERE candidate.import_item_id=i.id
AND candidate.source_snapshot_id=d.effective_source_snapshot_id
AND candidate.target_platform_instance_id=d.target_platform_instance_id
AND candidate.core_artifact_id=current_artifact.id
ORDER BY candidate.created_at_ms DESC,
candidate.id DESC LIMIT 1)
WHERE i.id=?
AND i.state='REVIEW_PENDING'
AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items pegasus
  WHERE pegasus.library_import_item_id=i.id AND pegasus.execution_state<>'REVIEW_PENDING'
)
`, request.PathValue("importItemId")).
		Scan(
			&itemID,
			&importJobID,
			&metadata,
			&version,
			&updatedAtMS,
			&platformID,
			&platformName,
			&currentArtifactCompatibility,
			&validationID,
			&validationStatus,
			&compatibilityCode,
			&dependencySnapshot,
			&selectedValidationID,
			&sourceSnapshotID,
			&sourceManifest,
			&sourceContentKind,
			&validationGeneration,
			&selectedCandidateID,
			&coverID,
			&uploadedCoverID,
			&backgroundID,
			&defaultDOSEntry,
		)
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var metadataValue, sourceValue any
	_ = json.Unmarshal([]byte(metadata), &metadataValue)
	_ = json.Unmarshal([]byte(sourceManifest), &sourceValue)
	if files, ok := sourceValue.([]any); ok {
		sourceValue = map[string]any{"files": files}
	}
	dependencyValue := decodeOptionalJSON(dependencySnapshot)
	candidates, scrapeRuns, err := server.reviewMetadataEvidence(request, itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	uploadedAssets, err := server.reviewUploadedAssets(request, itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	sourceMedia, err := server.optionalReviewServerSourceMedia(request.Context(), itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	sourceFiles, err := server.reviewSourceFiles(request, sourceSnapshotID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	screenshotIDs, err := server.reviewScreenshotIDs(request.Context(), itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	dosEntries, err := server.reviewDOSEntries(request.Context(), itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	duplicateGames, contentIdentityDigest, err := server.importer.DuplicateGames(request.Context(), itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	arcadeDependencies, multiDisc, err := server.reviewContentDependencies(request.Context(), itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	validationProjection, runtimeScreenshot, err := server.reviewValidationEvidence(
		request.Context(), itemID, reviewValidationInput{
			validationID: validationID, validationStatus: validationStatus,
			compatibilityCode: compatibilityCode, dependencyValue: dependencyValue,
			validationGeneration: validationGeneration, selectedValidationID: selectedValidationID,
			artifactCompatibility: currentArtifactCompatibility, sourceContentKind: sourceContentKind,
		})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	gateReviewMultiDiscAttachment(multiDisc, validationProjection.stale)
	canApprove := validationProjection.canApprove || runtimeScreenshot.value != nil
	reviewTags, err := server.activeReviewTags(request.Context(), itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(writer, http.StatusOK, map[string]any{
		"itemId": itemID, "importJobId": importJobID, "version": version, "updatedAtMs": updatedAtMS,
		"effectiveSourceSnapshotId": sourceSnapshotID,
		"platformInstance": map[string]any{
			"id":   platformID,
			"name": platformName,
		}, "metadata": metadataValue, "sourceManifest": sourceValue,
		"validation": validationProjection.value, "candidates": candidates, "scrapeRuns": scrapeRuns,
		"validationStale":              validationProjection.stale,
		"selectedValidationGeneration": validationProjection.selectedGeneration,
		"canApprove":                   canApprove,
		"uploadedAssets":               uploadedAssets, "sourceFiles": sourceFiles,
		"sourceMedia":       sourceMedia.value,
		"runtimeScreenshot": runtimeScreenshot.value,
		"duplicateGames":    duplicateGames, "contentIdentityDigest": contentIdentityDigest,
		"arcadeDependencies":  arcadeDependencies,
		"multiDisc":           multiDisc,
		"selectedCandidateId": nullableString(selectedCandidateID),
		"defaultDosEntry":     nullableString(defaultDOSEntry),
		"selectedAssets": map[string]any{
			"coverCandidateAssetId":       nullableString(coverID),
			"coverUploadedAssetId":        nullableString(uploadedCoverID),
			"backgroundCandidateAssetId":  nullableString(backgroundID),
			"screenshotCandidateAssetIds": screenshotIDs,
		}, "dosEntries": dosEntries, "tags": reviewTags,
	})
}

type optionalReviewProjection struct {
	value any
}

func (server *Server) reviewRuntimeScreenshot(
	ctx context.Context,
	itemID string,
	validationID sql.NullString,
	validationCurrent bool,
) (optionalReviewProjection, error) {
	if !validationCurrent || !validationID.Valid {
		return optionalReviewProjection{}, nil
	}
	var id, coreArtifactID string
	var width, height, capturedAtMS int64
	err := server.database.QueryRowContext(ctx, `
SELECT screenshot.id,screenshot.core_artifact_id,screenshot.width_px,screenshot.height_px,screenshot.captured_at_ms
FROM review_runtime_screenshots screenshot
JOIN review_drafts draft ON draft.import_item_id=screenshot.import_item_id
WHERE screenshot.import_item_id=? AND screenshot.validation_id=?
AND screenshot.source_snapshot_id=draft.effective_source_snapshot_id
`, itemID, validationID.String).Scan(&id, &coreArtifactID, &width, &height, &capturedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return optionalReviewProjection{}, nil
	}
	if err != nil {
		return optionalReviewProjection{}, fmt.Errorf("review runtime screenshot: %w", err)
	}
	return optionalReviewProjection{value: map[string]any{
		"screenshotId": id, "validationId": validationID.String, "coreArtifactId": coreArtifactID,
		"widthPx": width, "heightPx": height, "capturedAfterMs": int64(5_000),
		"capturedAtMs": capturedAtMS, "url": "/api/v1/admin/review-assets/" + id,
	}}, nil
}

func (server *Server) optionalReviewServerSourceMedia(
	ctx context.Context,
	itemID string,
) (optionalReviewProjection, error) {
	value, found, err := server.reviewServerSourceMedia(ctx, itemID)
	if err != nil {
		return optionalReviewProjection{}, err
	}
	if !found {
		return optionalReviewProjection{}, nil
	}
	return optionalReviewProjection{value: value}, nil
}

func (server *Server) reviewServerSourceMedia(ctx context.Context, itemID string) (any, bool, error) {
	var sourceRefID, importID, collectionName string
	var hasCover, hasVideo int
	var coverWidth, coverHeight sql.NullInt64
	err := server.database.QueryRowContext(ctx, `
SELECT pegasus.id,pegasus.import_id,COALESCE(collection.name,''),
EXISTS(
 SELECT 1 FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='COVER' AND asset.state='COPIED'
AND asset.blob_id IS NOT NULL
),
(
 SELECT asset.width_px FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='COVER' AND asset.state='COPIED'
),
(
 SELECT asset.height_px FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='COVER' AND asset.state='COPIED'
),
EXISTS(
 SELECT 1 FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='VIDEO' AND asset.state='COPIED'
 AND asset.blob_id IS NOT NULL
)
FROM pegasus_import_items pegasus
LEFT JOIN pegasus_import_collections collection ON collection.id=pegasus.collection_id
WHERE pegasus.library_import_item_id=?
`, itemID).Scan(
		&sourceRefID, &importID, &collectionName, &hasCover, &coverWidth, &coverHeight, &hasVideo,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("review source media: %w", err)
	}
	sourceLabel := any(nil)
	if collectionName != "" {
		sourceLabel = collectionName
	}
	result := map[string]any{
		"sourceKind": "PEGASUS", "sourceRefId": sourceRefID, "pegasusImportId": importID,
		"sourceLabel": sourceLabel, "coverUrl": nil, "coverWidthPx": nil, "coverHeightPx": nil,
		"videoUrl": nil,
	}
	baseURL := "/api/v1/admin/review-assets/" + sourceRefID
	if hasCover == 1 {
		result["coverUrl"] = baseURL + "?kind=COVER"
		result["coverWidthPx"] = nullableInt64(coverWidth)
		result["coverHeightPx"] = nullableInt64(coverHeight)
	}
	if hasVideo == 1 {
		result["videoUrl"] = baseURL + "?kind=VIDEO"
	}
	return result, true, nil
}

type reviewValidationInput struct {
	validationID, validationStatus, compatibilityCode, selectedValidationID sql.NullString
	validationGeneration                                                    sql.NullInt64
	dependencyValue                                                         any
	artifactCompatibility, sourceContentKind                                string
}

type reviewValidationResult struct {
	value, selectedGeneration any
	stale, canApprove         bool
}

func (server *Server) reviewValidationProjection(
	ctx context.Context,
	input reviewValidationInput,
) (reviewValidationResult, error) {
	if !input.validationID.Valid {
		return reviewValidationResult{}, nil
	}
	evidenceCurrent, err := server.importer.ReviewValidationCurrent(ctx, input.validationID.String)
	if err != nil {
		return reviewValidationResult{}, fmt.Errorf("review validation projection: %w", err)
	}
	selectedGeneration := any(nil)
	if input.selectedValidationID.Valid {
		selectedGeneration = nullableInt64(input.validationGeneration)
	}
	return reviewValidationResult{
		value: map[string]any{
			"id": input.validationID.String, "status": input.validationStatus.String,
			"current":            evidenceCurrent && input.validationStatus.String == "READY",
			"generation":         nullableInt64(input.validationGeneration),
			"compatibilityCode":  input.compatibilityCode.String,
			"dependencySnapshot": input.dependencyValue,
		},
		selectedGeneration: selectedGeneration,
		stale:              !evidenceCurrent,
		canApprove: input.selectedValidationID.Valid && evidenceCurrent && input.validationStatus.String == "READY" &&
			contentcapability.SupportsContentKind(input.artifactCompatibility, input.sourceContentKind),
	}, nil
}

func (server *Server) reviewValidationEvidence(
	ctx context.Context,
	itemID string,
	input reviewValidationInput,
) (reviewValidationResult, optionalReviewProjection, error) {
	projection, err := server.reviewValidationProjection(ctx, input)
	if err != nil {
		return reviewValidationResult{}, optionalReviewProjection{}, err
	}
	screenshot, err := server.reviewRuntimeScreenshot(ctx, itemID, input.validationID, !projection.stale)
	if err != nil {
		return reviewValidationResult{}, optionalReviewProjection{}, err
	}
	return projection, screenshot, nil
}

func (server *Server) reviewContentDependencies(ctx context.Context, itemID string) (any, any, error) {
	arcadeDependencies, hasArcadeDependencies, err := server.importer.ReviewArcadeDependencies(ctx, itemID)
	if err != nil {
		return nil, nil, fmt.Errorf("review arcade dependencies: %w", err)
	}
	if !hasArcadeDependencies {
		arcadeDependencies = nil
	}
	multiDisc, hasMultiDisc, err := server.importer.ReviewMultiDisc(ctx, itemID)
	if err != nil {
		return nil, nil, fmt.Errorf("review multi-disc dependencies: %w", err)
	}
	if !hasMultiDisc {
		multiDisc = nil
	}
	return arcadeDependencies, multiDisc, nil
}

func gateReviewMultiDiscAttachment(value any, validationStale bool) {
	projection, ok := value.(map[string]any)
	if !ok {
		return
	}
	canAttach, ok := projection["canAttachMissingDiscs"].(bool)
	if !ok {
		projection["canAttachMissingDiscs"] = false
		return
	}
	projection["canAttachMissingDiscs"] = canAttach && !validationStale
}

func (server *Server) reviewMetadataEvidence(
	request *http.Request,
	itemID string,
) ([]map[string]any, []map[string]any, error) {
	candidates, err := server.reviewCandidates(request, itemID)
	if err != nil {
		return nil, nil, err
	}
	runs, err := server.reviewScrapeRuns(request, itemID)
	if err != nil {
		return nil, nil, err
	}
	return candidates, runs, nil
}

func (server *Server) reviewScreenshotIDs(ctx context.Context, itemID string) ([]string, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT s.candidate_asset_id
FROM review_draft_screenshot_assets s
JOIN review_drafts d ON d.id=s.review_draft_id
WHERE d.import_item_id=?
ORDER BY s.ordinal
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review screenshots: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan review screenshot: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review screenshots: %w", err)
	}
	return ids, nil
}

func (server *Server) reviewDOSEntries(ctx context.Context, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe
FROM import_item_dos_entries
WHERE import_item_id=?
ORDER BY rank,normalized_path
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review DOS entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0)
	for rows.Next() {
		var path, originalPath, kind string
		var rank, enabled, directSafe int64
		if err := rows.Scan(&path, &originalPath, &kind, &rank, &enabled, &directSafe); err != nil {
			return nil, fmt.Errorf("scan review DOS entry: %w", err)
		}
		entries = append(entries, map[string]any{
			"path": path, "originalPath": originalPath, "kind": kind, "rank": rank,
			"enabled": enabled == 1, "directLaunchSafe": directSafe == 1,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review DOS entries: %w", err)
	}
	return entries, nil
}

func (server *Server) reviewUploadedAssets(request *http.Request, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(request.Context(), `
SELECT id,kind,width_px,height_px,media_type,created_at_ms
FROM review_uploaded_assets
WHERE import_item_id=?
ORDER BY created_at_ms,id
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query uploaded review assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]map[string]any, 0)
	for rows.Next() {
		var id, kind, mediaType string
		var width, height, createdAtMS int64
		if err := rows.Scan(&id, &kind, &width, &height, &mediaType, &createdAtMS); err != nil {
			return nil, fmt.Errorf("scan uploaded review asset: %w", err)
		}
		assets = append(assets, map[string]any{
			"assetId": id, "kind": kind, "widthPx": width, "heightPx": height,
			"mediaType": mediaType, "url": "/api/v1/admin/review-assets/" + id, "createdAtMs": createdAtMS,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan uploaded review assets: %w", err)
	}
	return assets, nil
}

func (server *Server) reviewSourceFiles(request *http.Request, sourceSnapshotID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(request.Context(), `
SELECT f.id,f.relative_path,b.size_bytes,b.sha256,b.md5,b.crc32,
MAX(CASE WHEN s.source_archive_blob_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM archive_entries ae WHERE ae.archive_blob_id=f.final_blob_id
) THEN 1 ELSE 0 END),
COALESCE(
  MAX(s.source_archive_blob_id),
  MAX(CASE WHEN EXISTS(
    SELECT 1 FROM archive_entries ae WHERE ae.archive_blob_id=f.final_blob_id
  ) THEN f.final_blob_id END)
)
FROM import_item_source_snapshot_files s
JOIN upload_files f ON f.id=s.upload_file_id
JOIN blobs b ON b.id=f.final_blob_id
WHERE s.source_snapshot_id=?
GROUP BY f.id,f.relative_path,b.size_bytes,b.sha256,b.md5,b.crc32
ORDER BY min(s.sort_order),f.relative_path,f.id
`, sourceSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("query review source files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type sourceRow struct {
		id, name, sha256, md5, crc32 string
		size                         int64
		archive                      int64
		archiveBlobID                sql.NullString
	}
	records := make([]sourceRow, 0)
	for rows.Next() {
		var record sourceRow
		if err := rows.Scan(&record.id, &record.name, &record.size, &record.sha256, &record.md5,
			&record.crc32, &record.archive, &record.archiveBlobID); err != nil {
			return nil, fmt.Errorf("scan review source file: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review source files: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close review source files: %w", err)
	}
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		var entries []map[string]any
		var archiveFormat any
		if record.archiveBlobID.Valid {
			entries, archiveFormat, err = server.reviewArchiveEntries(request.Context(), record.archiveBlobID.String)
			if err != nil {
				return nil, err
			}
		} else {
			entries = make([]map[string]any, 0)
		}
		result = append(result, map[string]any{
			"uploadFileId": record.id, "name": record.name, "sizeBytes": record.size,
			"sha256": record.sha256, "md5": record.md5, "crc32": record.crc32,
			"archive": record.archive == 1, "archiveFormat": archiveFormat, "archiveEntries": entries,
		})
	}
	return result, nil
}

func (server *Server) reviewArchiveEntries(ctx context.Context, archiveBlobID string) ([]map[string]any, any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT original_relative_path,uncompressed_size_bytes,crc32,archive_format
FROM archive_entries
WHERE archive_blob_id=?
ORDER BY ordinal
`, archiveBlobID)
	if err != nil {
		return nil, nil, fmt.Errorf("query review archive entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0)
	var archiveFormat any
	for rows.Next() {
		var name, crc32, format string
		var size int64
		if err := rows.Scan(&name, &size, &crc32, &format); err != nil {
			return nil, nil, fmt.Errorf("scan review archive entry: %w", err)
		}
		archiveFormat = format
		entries = append(entries, map[string]any{"name": name, "sizeBytes": size, "crc32": crc32})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan review archive entries: %w", err)
	}
	return entries, archiveFormat, nil
}

func (server *Server) reviewScrapeRuns(request *http.Request, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
WITH evidence_counts AS (
  SELECT scrape_run_id,
  COUNT(*) AS evidence_count
  FROM content_hash_evidence
  GROUP BY scrape_run_id
), candidate_counts AS (
  SELECT scrape_run_id,
  COUNT(*) AS candidate_count
  FROM scrape_candidates
  GROUP BY scrape_run_id
), outcome_counts AS (
  SELECT a.scrape_run_id,
  COUNT(*) AS attempt_count,
  SUM(CASE WHEN p.outcome='HIT' THEN 1 ELSE 0 END) AS hit,
  SUM(CASE WHEN p.outcome='MISS' THEN 1 ELSE 0 END) AS miss,
  SUM(CASE WHEN p.outcome='RATE_LIMITED' THEN 1 ELSE 0 END) AS rate_limited,
  SUM(CASE WHEN p.outcome='TIMEOUT' THEN 1 ELSE 0 END) AS timeout,
  SUM(CASE WHEN p.outcome='INVALID_RESPONSE' THEN 1 ELSE 0 END) AS invalid_response,
  SUM(CASE WHEN p.outcome='NETWORK_ERROR' THEN 1 ELSE 0 END) AS network_error
  FROM metadata_scrape_query_attempts a
  JOIN metadata_provider_responses p ON p.id=a.provider_response_id
  GROUP BY a.scrape_run_id
)
SELECT r.id,
r.job_id,
r.provider,
r.state,
j.state,
r.created_at_ms,
r.completed_at_ms,
r.error_code,
COALESCE(e.evidence_count,0),
COALESCE(o.attempt_count,0),
COALESCE(c.candidate_count,0),
COALESCE(o.hit,0),
COALESCE(o.miss,0),
COALESCE(o.rate_limited,0),
COALESCE(o.timeout,0),
COALESCE(o.invalid_response,0),
COALESCE(o.network_error,0)
FROM metadata_scrape_runs r
JOIN jobs j ON j.id=r.job_id
LEFT JOIN evidence_counts e ON e.scrape_run_id=r.id
LEFT JOIN candidate_counts c ON c.scrape_run_id=r.id
LEFT JOIN outcome_counts o ON o.scrape_run_id=r.id
WHERE r.import_item_id=?
ORDER BY r.created_at_ms DESC,
r.id DESC
LIMIT 10
`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("query review scrape runs: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	runs := make([]map[string]any, 0)
	for rows.Next() {
		run, scanErr := scanReviewScrapeRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review scrape runs: %w", err)
	}
	return runs, nil
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanReviewScrapeRun(scanner rowScanner) (map[string]any, error) {
	var runID, jobID, provider, state, jobState string
	var createdAtMS, evidenceCount, attemptCount, candidateCount int64
	var completedAtMS sql.NullInt64
	var errorCode sql.NullString
	var hit, miss, rateLimited, timeout, invalidResponse, networkError int64
	if err := scanner.Scan(
		&runID,
		&jobID,
		&provider,
		&state,
		&jobState,
		&createdAtMS,
		&completedAtMS,
		&errorCode,
		&evidenceCount,
		&attemptCount,
		&candidateCount,
		&hit,
		&miss,
		&rateLimited,
		&timeout,
		&invalidResponse,
		&networkError,
	); err != nil {
		return nil, fmt.Errorf("scan review scrape run: %w", err)
	}
	return map[string]any{
		"scrapeRunId":    runID,
		"jobId":          jobID,
		"provider":       provider,
		"state":          state,
		"jobState":       jobState,
		"createdAtMs":    createdAtMS,
		"completedAtMs":  nullableInt64(completedAtMS),
		"errorCode":      nullableString(errorCode),
		"evidenceCount":  evidenceCount,
		"attemptCount":   attemptCount,
		"candidateCount": candidateCount,
		"outcomes": map[string]any{
			"hit":             hit,
			"miss":            miss,
			"rateLimited":     rateLimited,
			"timeout":         timeout,
			"invalidResponse": invalidResponse,
			"networkError":    networkError,
		},
	}, nil
}

func (server *Server) reviewCandidates(request *http.Request, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT c.id,
c.scrape_run_id,
c.provider_game_id,
c.normalized_metadata_json,
c.evidence_json,
c.created_at_ms
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.import_item_id=?
AND r.state='COMPLETED'
ORDER BY r.created_at_ms DESC,
c.created_at_ms,
c.id
`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("httpapi/server: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type candidateRow struct {
		id, runID, providerGameID, metadataJSON, evidenceJSON string
		createdAtMS                                           int64
	}
	records := make([]candidateRow, 0)
	for rows.Next() {
		var record candidateRow
		if err := rows.Scan(
			&record.id,
			&record.runID,
			&record.providerGameID,
			&record.metadataJSON,
			&record.evidenceJSON,
			&record.createdAtMS,
		); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close review candidates: %w", err)
	}
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		var metadataValue, evidenceValue any
		if err := json.Unmarshal([]byte(record.metadataJSON), &metadataValue); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		if err := json.Unmarshal([]byte(record.evidenceJSON), &evidenceValue); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		assets, err := server.reviewCandidateAssets(request, record.id)
		if err != nil {
			return nil, err
		}
		result = append(
			result,
			map[string]any{
				"candidateId":    record.id,
				"scrapeRunId":    record.runID,
				"providerGameId": record.providerGameID,
				"metadata":       metadataValue,
				"evidence":       evidenceValue,
				"assets":         assets,
				"createdAtMs":    record.createdAtMS,
			},
		)
	}
	return result, nil
}

func (server *Server) reviewCandidateAssets(request *http.Request, candidateID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT id,
provider_asset_id,
kind_hint,
ordinal,
status,
width_px,
height_px,
media_type,
error_code
FROM scrape_candidate_assets
WHERE scrape_candidate_id=?
ORDER BY kind_hint,
ordinal,
id
`,
		candidateID,
	)
	if err != nil {
		return nil, fmt.Errorf("httpapi/server: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]map[string]any, 0)
	for rows.Next() {
		var id, providerAssetID, kind, status string
		var ordinal int64
		var width, height sql.NullInt64
		var mediaType, errorCode sql.NullString
		if err := rows.Scan(
			&id,
			&providerAssetID,
			&kind,
			&ordinal,
			&status,
			&width,
			&height,
			&mediaType,
			&errorCode,
		); err != nil {
			return nil, fmt.Errorf("httpapi/server: %w", err)
		}
		assets = append(
			assets,
			map[string]any{
				"candidateAssetId": id,
				"providerAssetId":  providerAssetID,
				"kind":             kind,
				"ordinal":          ordinal,
				"status":           status,
				"widthPx":          nullableInt64(width),
				"heightPx":         nullableInt64(height),
				"mediaType":        nullableString(mediaType),
				"errorCode":        nullableString(errorCode),
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("httpapi/server: %w", err)
	}
	return assets, nil
}

func (server *Server) approveReview(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		Reason              *string  `json:"reason"`
		DuplicatePolicy     string   `json:"duplicatePolicy"`
		AcknowledgedGameIDs []string `json:"acknowledgedGameIds"`
	}
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核决定无效", map[string]any{})
		return
	}
	approved, err := server.importer.ApproveWithDecision(
		request.Context(),
		request.PathValue("importItemId"),
		version,
		libraryimport.ApprovalDecision{
			Reason:              body.Reason,
			DuplicatePolicy:     body.DuplicatePolicy,
			AcknowledgedGameIDs: body.AcknowledgedGameIDs,
		},
	)
	if err != nil {
		var duplicateConflict *libraryimport.DuplicateConflict
		if errors.As(err, &duplicateConflict) {
			writeError(
				writer,
				request,
				http.StatusConflict,
				"DUPLICATE_GAME_CONFIRMATION_REQUIRED",
				"相同游戏文件已关联到已发布游戏；继续发布可能产生重复游戏",
				map[string]any{
					"contentIdentityDigest": duplicateConflict.ContentIdentityDigest,
					"games":                 duplicateConflict.Games,
				},
			)
			return
		}
		writeError(writer, request, http.StatusConflict, "REVIEW_VALIDATION_STALE", "审核输入或验证结果已经变化", map[string]any{})
		return
	}
	writeJSON(writer, http.StatusCreated, approved)
}

func (server *Server) createUpload(writer http.ResponseWriter, request *http.Request) {
	var body uploads.CreateRequest
	if err := decodeJSON(writer, request, &body, 2<<20); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "上传清单无效", map[string]any{})
		return
	}
	session, err := server.uploads.Create(request.Context(), body)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "上传清单无效", map[string]any{})
		return
	}
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(writer, http.StatusCreated, session)
}

func (server *Server) getUpload(writer http.ResponseWriter, request *http.Request) {
	session, err := server.uploads.Get(request.Context(), request.PathValue("uploadId"))
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, session.Version))
	writeJSON(writer, http.StatusOK, session)
}

func (server *Server) putUploadPart(writer http.ResponseWriter, request *http.Request) {
	partNo, err := strconv.Atoi(request.PathValue("partNo"))
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "分块编号无效", map[string]any{})
		return
	}
	body := http.MaxBytesReader(writer, request.Body, uploads.PartSize+1)
	err = server.uploads.PutPart(
		request.Context(),
		request.PathValue("uploadId"),
		request.PathValue("fileId"),
		partNo,
		request.Header.Get("Content-Range"),
		request.Header.Get("Content-Digest"),
		body,
	)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusBadRequest,
			"UPLOAD_RANGE_MISMATCH",
			"上传分块校验失败",
			map[string]any{"partNo": partNo},
		)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) completeUpload(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"缺少有效资源版本",
			map[string]any{},
		)
		return
	}
	jobID, finalization, err := server.uploads.Complete(request.Context(), request.PathValue("uploadId"), version)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "上传状态或版本已经变化", map[string]any{})
		return
	}
	writeJSON(
		writer,
		http.StatusAccepted,
		map[string]any{
			"uploadId":       request.PathValue("uploadId"),
			"jobId":          jobID,
			"finalizationNo": finalization,
			"state":          "FINALIZING",
		},
	)
}

func (server *Server) cancelUpload(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前上传版本",
			map[string]any{},
		)
		return
	}
	result, pending, err := server.uploads.Cancel(request.Context(), request.PathValue("uploadId"), version)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "UPLOAD_CANCEL_CONFLICT", "上传状态或版本已经变化", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	if pending {
		writeJSON(writer, http.StatusAccepted, result)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) job(writer http.ResponseWriter, request *http.Request) {
	var id, scopeType, scopeID, kind, state string
	var version, attempts, maxAttempts, updatedAtMS int64
	var errorCode sql.NullString
	var retryable sql.NullInt64
	err := server.database.QueryRowContext(request.Context(), `
SELECT id,
scope_type,
scope_id,
kind,
state,
version,
attempt_count,
max_attempts,
error_code,
error_retryable,
updated_at_ms
FROM jobs
WHERE id=?
`, request.PathValue("jobId")).
		Scan(
			&id,
			&scopeType,
			&scopeID,
			&kind,
			&state,
			&version,
			&attempts,
			&maxAttempts,
			&errorCode,
			&retryable,
			&updatedAtMS,
		)
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"jobId":        id,
			"scopeType":    scopeType,
			"scopeId":      scopeID,
			"kind":         kind,
			"state":        state,
			"version":      version,
			"attemptCount": attempts,
			"maxAttempts":  maxAttempts,
			"errorCode":    nullableString(errorCode),
			"retryable":    retryable.Valid && retryable.Int64 == 1,
			"updatedAtMs":  updatedAtMS,
		},
	)
}

func (server *Server) jobEvents(writer http.ResponseWriter, request *http.Request) {
	server.streamJobEvents(writer, request, request.PathValue("jobId"))
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any, limit int64) error {
	if mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err != nil ||
		mediaType != "application/json" {
		return errJSONContentType
	}
	contents, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, limit))
	if err != nil {
		return fmt.Errorf("httpapi/server: %w", err)
	}
	if !utf8.Valid(contents) {
		return errJSONUTF8
	}
	if err := validateJSONLexical(contents, 64); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("httpapi/server: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errJSONTrailing
		}
		return fmt.Errorf("httpapi/server: %w", err)
	}
	return nil
}

//nolint:gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func validateJSONLexical(contents []byte, maxDepth int) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var parseValue func(int) error
	parseValue = func(depth int) error {
		if depth > maxDepth {
			return errJSONNesting
		}
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("httpapi/server: %w", err)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			keys := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("httpapi/server: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return errJSONObjectKey
				}
				if _, exists := keys[key]; exists {
					return errJSONDuplicateKey
				}
				keys[key] = struct{}{}
				if err := parseValue(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := parseValue(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errJSONDelimiter
		}
		closing, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("httpapi/server: %w", err)
		}
		if expected := map[json.Delim]json.Delim{'{': '}', '[': ']'}[delimiter]; closing != expected {
			return errJSONClosingDelimiter
		}
		return nil
	}
	if err := parseValue(1); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errJSONTrailing
		}
		return fmt.Errorf("httpapi/server: %w", err)
	}
	return nil
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func gameCoverURL(assetID sql.NullString) any {
	if assetID.Valid {
		return "/content/assets/" + assetID.String
	}
	return nil
}

func reviewAssetURL(assetID sql.NullString) any {
	if assetID.Valid {
		return "/api/v1/admin/review-assets/" + assetID.String
	}
	return nil
}

func saveStateScreenshotURL(saveStateID string) string {
	return "/content/save-states/" + saveStateID + "/screenshot"
}

func nullableInt64(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func (server *Server) reviewCandidateAsset(writer http.ResponseWriter, request *http.Request) {
	var digest, mediaType string
	err := server.database.QueryRowContext(request.Context(), `
SELECT digest,media_type FROM (
  SELECT b.sha256 AS digest,a.media_type AS media_type
  FROM scrape_candidate_assets a
  JOIN blobs b ON b.id=a.blob_id
  JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
  JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
  LEFT JOIN import_items i ON i.id=r.import_item_id
  LEFT JOIN games g ON g.id=r.game_id
  WHERE a.id=? AND a.status='READY'
  AND (i.state='REVIEW_PENDING' OR g.status='PUBLISHED' OR EXISTS (
    SELECT 1 FROM review_events e WHERE e.import_item_id=i.id AND e.event_type IN ('APPROVED','DISCARDED')
  ))
  UNION ALL
  SELECT b.sha256 AS digest,a.media_type AS media_type
  FROM review_uploaded_assets a
  JOIN blobs b ON b.id=a.blob_id
  JOIN import_items i ON i.id=a.import_item_id
  WHERE a.id=?
  AND (i.state='REVIEW_PENDING' OR EXISTS (
    SELECT 1 FROM review_events e WHERE e.import_item_id=i.id AND e.event_type IN ('APPROVED','DISCARDED')
  ))
  UNION ALL
  SELECT b.sha256 AS digest,screenshot.media_type AS media_type
  FROM review_runtime_screenshots screenshot
  JOIN blobs b ON b.id=screenshot.blob_id
  JOIN import_items i ON i.id=screenshot.import_item_id
  WHERE screenshot.id=?
  AND (i.state='REVIEW_PENDING' OR EXISTS (
    SELECT 1 FROM review_events e WHERE e.import_item_id=i.id AND e.event_type IN ('APPROVED','DISCARDED')
  ))
) LIMIT 1
`, request.PathValue("assetId"), request.PathValue("assetId"), request.PathValue("assetId")).Scan(&digest, &mediaType)
	if errors.Is(err, sql.ErrNoRows) {
		kind := request.URL.Query().Get("kind")
		if kind == "" {
			kind = "COVER"
		}
		if kind != "COVER" && kind != "VIDEO" {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "审核来源媒体类型无效", map[string]any{})
			return
		}
		err = server.database.QueryRowContext(request.Context(), `
SELECT blob.sha256,asset.media_type
FROM pegasus_import_item_assets asset
JOIN blobs blob ON blob.id=asset.blob_id
JOIN pegasus_import_items pegasus ON pegasus.id=asset.item_id
JOIN import_items item ON item.id=pegasus.library_import_item_id
WHERE pegasus.id=? AND asset.kind=? AND asset.state='COPIED'
AND (item.state='REVIEW_PENDING' OR EXISTS(
  SELECT 1 FROM review_events event
  WHERE event.import_item_id=item.id AND event.event_type IN ('APPROVED','DISCARDED')
))
`, request.PathValue("assetId"), kind).Scan(&digest, &mediaType)
	}
	if err != nil {
		writeError(writer, request, http.StatusNotFound, "REVIEW_ASSET_NOT_FOUND", "候选媒体不存在", map[string]any{})
		return
	}
	server.serveBlob(writer, request, digest, mediaType, true)
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (server *Server) diagnostics(writer http.ResponseWriter, request *http.Request) {
	transaction, err := server.database.BeginTx(request.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer cleanup.Rollback(transaction)
	var schemaVersion int64
	if err := transaction.QueryRowContext(request.Context(), `
SELECT COALESCE(MAX(version),
0)
FROM schema_migrations
`).Scan(&schemaVersion); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var publishedGames, deletedGames, activeSaves, deletedSaves, blobCount int64
	var queuedJobs, runningJobs, cancelRequestedJobs, succeededJobs, failedJobs, cancelledJobs int64
	var pendingDATs, parsingDATs, readyDATs, failedDATs, cancelledDATs int64
	err = transaction.QueryRowContext(request.Context(), `
SELECT
(SELECT count(*)
FROM games
WHERE status='PUBLISHED'),
(SELECT count(*)
FROM games
WHERE status='DELETED'),
(SELECT count(*)
FROM save_states
WHERE deleted_at_ms IS NULL),
(SELECT count(*)
FROM save_states
WHERE deleted_at_ms IS NOT NULL),
(SELECT count(*)
FROM blobs),
(SELECT count(*)
FROM jobs
WHERE state='QUEUED'),
(SELECT count(*)
FROM jobs
WHERE state='RUNNING'),
(SELECT count(*)
FROM jobs
WHERE state='CANCEL_REQUESTED'),
(SELECT count(*)
FROM jobs
WHERE state='SUCCEEDED'),
(SELECT count(*)
FROM jobs
WHERE state='FAILED'),
(SELECT count(*)
FROM jobs
WHERE state='CANCELLED'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='PENDING'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='PARSING'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='READY'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='FAILED'),
(SELECT count(*)
FROM dat_versions
WHERE parse_status='CANCELLED')
`).Scan(
		&publishedGames, &deletedGames, &activeSaves, &deletedSaves, &blobCount,
		&queuedJobs, &runningJobs, &cancelRequestedJobs, &succeededJobs, &failedJobs, &cancelledJobs,
		&pendingDATs, &parsingDATs, &readyDATs, &failedDATs, &cancelledDATs,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	versions := make([]string, 0, len(server.dependencies.Versions))
	for version := range server.dependencies.Versions {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(left, right int) bool { return semverLess(versions[left], versions[right]) })
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Disposition", `attachment; filename="retrom-diagnostics.json"`)
	writeJSON(writer, http.StatusOK, map[string]any{
		"schemaVersion": 1, "generatedAtMs": server.now().UnixMilli(), "databaseSchemaVersion": schemaVersion,
		"dependencies": map[string]any{
			"configuredEmulatorjsVersions": versions,
			"activeEmulatorjsVersion":      server.config.ActiveEJSVersion,
		},
		"counts": map[string]any{
			"games":      map[string]any{"published": publishedGames, "deleted": deletedGames},
			"saveStates": map[string]any{"active": activeSaves, "deleted": deletedSaves},
			"blobs":      blobCount,
			"jobs": map[string]any{
				"queued":          queuedJobs,
				"running":         runningJobs,
				"cancelRequested": cancelRequestedJobs,
				"succeeded":       succeededJobs,
				"failed":          failedJobs,
				"cancelled":       cancelledJobs,
			},
			"datVersions": map[string]any{
				"pending":   pendingDATs,
				"parsing":   parsingDATs,
				"ready":     readyDATs,
				"failed":    failedDATs,
				"cancelled": cancelledDATs,
			},
		},
	})
}

func semverLess(left, right string) bool {
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < 3; index++ {
		var leftPart, rightPart int64
		if index < len(leftParts) {
			leftPart, _ = strconv.ParseInt(strings.SplitN(leftParts[index], "-", 2)[0], 10, 64)
		}
		if index < len(rightParts) {
			rightPart, _ = strconv.ParseInt(strings.SplitN(rightParts[index], "-", 2)[0], 10, 64)
		}
		if leftPart != rightPart {
			return leftPart < rightPart
		}
	}
	return left < right
}

func (server *Server) runtimeFile(writer http.ResponseWriter, request *http.Request) {
	versionName := request.PathValue("configuredVersion")
	runtimePath := request.PathValue("runtimePath")
	version := server.dependencies.Versions[versionName]
	if version == nil {
		server.notFound(writer, request)
		return
	}
	declaration, ok := version.Allowlist[runtimePath]
	if !ok {
		server.notFound(writer, request)
		return
	}
	path := filepath.Join(version.RuntimeRoot, filepath.FromSlash(runtimePath))
	file, err := os.Open(path) //nolint:gosec // runtimePath must match the versioned dependency allowlist exactly.
	if err != nil {
		server.notFound(writer, request)
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != declaration.SizeBytes {
		writeError(writer, request, http.StatusServiceUnavailable, "DEPENDENCY_INVALID", "运行时依赖不可用", map[string]any{})
		return
	}
	mediaType := mime.TypeByExtension(filepath.Ext(runtimePath))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("ETag", `"sha256-`+declaration.SHA256+`"`)
	http.ServeContent(writer, request, filepath.Base(runtimePath), time.Unix(0, 0), file)
}

func (server *Server) notFound(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "资源不存在", map[string]any{})
}

func (server *Server) databaseError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	slog.ErrorContext(
		request.Context(),
		"database operation failed",
		"requestId",
		request.Context().Value(requestIDKey),
		"error",
		err,
	)
	writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "数据库操作失败", map[string]any{})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	if writer.Header().Get("Cache-Control") == "" {
		writer.Header().Set("Cache-Control", "private, no-store")
	}
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(
	writer http.ResponseWriter,
	request *http.Request,
	status int,
	code, message string,
	details map[string]any,
) {
	requestID, _ := request.Context().Value(requestIDKey).(string)
	writeJSON(
		writer,
		status,
		map[string]any{
			"error": map[string]any{"code": code, "message": message, "details": details, "requestId": requestID},
		},
	)
}

// ParseETag accepts only the exact strong resource-version representation.
func ParseETag(value string) (int64, error) {
	if len(value) < 4 || !strings.HasPrefix(value, `"v`) || !strings.HasSuffix(value, `"`) {
		return 0, errInvalidETag
	}
	version, err := strconv.ParseInt(value[2:len(value)-1], 10, 64)
	if err != nil || version < 1 || fmt.Sprintf(`"v%d"`, version) != value {
		return 0, errInvalidETag
	}
	return version, nil
}
