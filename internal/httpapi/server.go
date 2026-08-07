package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
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
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"retrom/internal/arcadecatalog"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/cursor"
	"retrom/internal/dependencies"
	"retrom/internal/firmware"
	"retrom/internal/gamecontent"
	"retrom/internal/hasheous"
	"retrom/internal/jobs"
	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	"retrom/internal/metadatascrape"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/saves"
	"retrom/internal/uploads"
)

const csrfCookieName = "retrom_csrf"

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
)

type contextKey string

const requestIDKey contextKey = "request-id"

type Server struct {
	config       config.Config
	database     *sql.DB
	dependencies *dependencies.Set
	blobs        *blobstore.Store
	credentials  *retromruntime.Credentials
	cursors      *cursor.Codec
	uploads      *uploads.Service
	importer     *libraryimport.Service
	launcher     *launch.Service
	jobService   *jobs.Service
	firmware     *firmware.Service
	arcadeDAT    *arcadecatalog.Service
	metadata     *metadatascrape.Service
	gameContent  *gamecontent.Service
	saveService  *saves.Service
	now          func() time.Time
	sseHeartbeat time.Duration
	idempotency  sync.Mutex
}

func New(
	config config.Config,
	database *sql.DB,
	dependencySet *dependencies.Set,
	blobs *blobstore.Store,
	credentials *retromruntime.Credentials,
	now func() time.Time,
) *Server {
	scraper := metadatascrape.New(database, blobs, hasheous.New(nil, nil, now), now)
	launcher := launch.New(database, dependencySet, credentials, now)
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
	return &Server{
		config:       config,
		database:     database,
		dependencies: dependencySet,
		blobs:        blobs,
		credentials:  credentials,
		cursors:      cursor.New(credentials.CursorKey(), now),
		uploads:      uploads.New(database, blobs, config.DataDir, now),
		importer:     libraryimport.New(database, now, scraper).WithBlobStore(blobs),
		launcher:     launcher,
		jobService:   jobs.New(database, now),
		firmware:     firmware.New(database, now),
		arcadeDAT:    arcadeDAT,
		metadata:     scraper,
		gameContent:  gamecontent.New(database, now),
		saveService:  saves.New(database, blobs, credentials, now),
		now:          now,
		sseHeartbeat: 15 * time.Second,
	}
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", server.healthLive)
	mux.HandleFunc("GET /health/ready", server.healthReady)
	mux.HandleFunc("GET /api/v1/session", server.session)
	mux.HandleFunc("GET /api/v1/home", server.home)
	mux.HandleFunc("GET /api/v1/recent-games", server.recentGames)
	mux.HandleFunc("GET /api/v1/games", server.games)
	mux.HandleFunc("GET /api/v1/games/{gameId}", server.game)
	mux.HandleFunc("GET /api/v1/saves", server.saves)
	mux.HandleFunc("PATCH /api/v1/saves/{saveStateId}", server.patchSave)
	mux.HandleFunc("DELETE /api/v1/saves/{saveStateId}", server.deleteSave)
	mux.HandleFunc("POST /api/v1/launches", server.createLaunch)
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
	mux.HandleFunc("POST /api/v1/admin/arcade-dats/{datVersionId}/activate", server.activateArcadeDAT)
	mux.HandleFunc("POST /api/v1/admin/arcade-dats/{datVersionId}/rollback", server.rollbackArcadeDAT)
	mux.HandleFunc("DELETE /api/v1/admin/arcade-dats/{datVersionId}", server.deleteArcadeDAT)
	mux.HandleFunc("GET /api/v1/admin/bios", server.bios)
	mux.HandleFunc("POST /api/v1/admin/bios/{requirementId}/installations", server.installBIOS)
	mux.HandleFunc("GET /api/v1/admin/imports/summary", server.importSummary)
	mux.HandleFunc("GET /api/v1/admin/imports", server.imports)
	mux.HandleFunc("POST /api/v1/admin/imports", server.createImport)
	mux.HandleFunc("GET /api/v1/admin/imports/{importJobId}", server.importDetail)
	mux.HandleFunc("GET /api/v1/admin/imports/{importJobId}/events", server.importEvents)
	mux.HandleFunc("POST /api/v1/admin/imports/{importJobId}/cancel", server.cancelImport)
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
	mux.HandleFunc("GET /api/v1/admin/reviews/{importItemId}", server.review)
	mux.HandleFunc("PATCH /api/v1/admin/reviews/{importItemId}", server.patchReview)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/scrape-candidates", server.scrapeReview)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/assets", server.createReviewAsset)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/approve", server.approveReview)
	mux.HandleFunc("POST /api/v1/admin/reviews/{importItemId}/discard", server.discardReview)
	mux.HandleFunc("GET /api/v1/admin/review-history", server.reviewHistory)
	mux.HandleFunc("GET /api/v1/admin/review-history/{reviewEventId}", server.reviewHistoryEvent)
	mux.HandleFunc("GET /api/v1/admin/games", server.adminGames)
	mux.HandleFunc("GET /api/v1/admin/games/{gameId}", server.adminGame)
	mux.HandleFunc("PATCH /api/v1/admin/games/{gameId}", server.patchAdminGame)
	mux.HandleFunc("DELETE /api/v1/admin/games/{gameId}", server.deleteAdminGame)
	mux.HandleFunc("POST /api/v1/admin/games/{gameId}/assets", server.createGameAsset)
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
	mux.HandleFunc("GET /runtime/launches/{launchId}/dos-config/game.conf", server.launchDOSConfig)
	mux.HandleFunc("GET /runtime/launches/{launchId}/game/{logicalName}", server.launchGame)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/game/{logicalName}", server.launchGame)
	mux.HandleFunc("GET /runtime/launches/{launchId}/bios/bundle.zip", server.launchBIOSBundle)
	mux.HandleFunc("GET /runtime/launches/{launchId}/parent/bundle.zip", server.launchParentBundle)
	mux.HandleFunc("POST /runtime/launches/{launchId}/start", server.launchStart)
	mux.HandleFunc("POST /runtime/launches/{launchId}/heartbeat", server.launchHeartbeat)
	mux.HandleFunc("POST /runtime/launches/{launchId}/finish", server.launchFinish)
	mux.HandleFunc("POST /runtime/launches/{launchId}/save-states", server.createSaveState)
	mux.HandleFunc("GET /runtime/launches/{launchId}/persistent-save", server.getPersistentSave)
	mux.HandleFunc("PUT /runtime/launches/{launchId}/persistent-save", server.putPersistentSave)
	mux.HandleFunc("GET /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("HEAD /runtime/launches/{launchId}/state", server.launchState)
	mux.HandleFunc("/", server.notFound)
	return server.baseMiddleware(server.openAPIHandler(mux))
}

func (server *Server) baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID, err := uuid.NewV7()
		if err != nil {
			http.Error(writer, "request id unavailable", http.StatusInternalServerError)
			return
		}
		requestContext := context.WithValue(request.Context(), requestIDKey, requestID.String())
		request = request.WithContext(requestContext)
		writer.Header().Set("X-Request-ID", requestID.String())
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "same-origin")
		writer.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		writer.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		if request.URL.Path != "/health/live" && request.URL.Path != "/health/ready" {
			reason := server.readinessReason(requestContext)
			if reason != "" {
				writeError(
					writer,
					request,
					http.StatusServiceUnavailable,
					"SERVICE_NOT_READY",
					"依赖索引尚未就绪",
					map[string]any{"reasonCode": reason},
				)
				return
			}
		}
		if err := validateQuery(request); err != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "查询参数无效", map[string]any{})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

//nolint:gocognit,gocyclo // Query branches encode independent per-operation allowlists and stable lexical constraints.
func validateQuery(request *http.Request) error {
	allowed := map[string]struct{}{}
	add := func(names ...string) {
		for _, name := range names {
			allowed[name] = struct{}{}
		}
	}
	path := request.URL.Path
	switch {
	case request.Method == http.MethodGet && path == "/api/v1/recent-games":
		add("limit")
	case request.Method == http.MethodGet && path == "/api/v1/games":
		add("q", "platformId", "platformInstanceId", "sort", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/saves":
		add("q", "gameId", "platformId", "platformInstanceId", "coreId", "availability", "sort", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/admin/imports":
		add("q", "state", "platformInstanceId", "sort", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/admin/reviews":
		add("q", "importJobId", "platformInstanceId", "blockerCode", "sort", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/admin/review-history":
		add("q", "decision", "platformInstanceId", "fromAtMs", "toAtMs", "sort", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/admin/games":
		add("q", "platformId", "platformInstanceId", "status", "sort", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/admin/platform-instances":
		add("platformId", "enabled", "sort", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/admin/bios":
		add("q", "platformId", "coreId", "coreArtifactId", "scope", "status", "cursor", "limit")
	case request.Method == http.MethodGet && path == "/api/v1/admin/arcade-dats":
		add("q", "coreId", "coreArtifactId", "source", "parseStatus", "cursor", "limit")
	case request.Method == http.MethodGet &&
		strings.HasPrefix(path, "/api/v1/admin/arcade-dats/") && strings.HasSuffix(path, "/diff"):
		add("section", "change", "cursor", "limit")
	}
	values := request.URL.Query()
	for name, entries := range values {
		if _, ok := allowed[name]; !ok || len(entries) != 1 {
			return errUnknownQuery
		}
	}
	if value := values.Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 || strconv.Itoa(parsed) != value {
			return errInvalidLimit
		}
	}
	if len(values.Get("cursor")) > 8192 || len([]rune(strings.TrimSpace(values.Get("q")))) > 200 {
		return errQueryTooLong
	}
	if values.Has("fromAtMs") && values.Has("toAtMs") {
		from, fromErr := strconv.ParseInt(values.Get("fromAtMs"), 10, 64)
		to, toErr := strconv.ParseInt(values.Get("toAtMs"), 10, 64)
		if fromErr != nil || toErr != nil || from > to {
			return errInvalidTimeRange
		}
	}
	return nil
}

func (server *Server) healthLive(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ok"})
}

func (server *Server) healthReady(writer http.ResponseWriter, request *http.Request) {
	if reason := server.readinessReason(request.Context()); reason != "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reasonCode": reason})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ready"})
}

func (server *Server) readinessReason(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := server.database.PingContext(ctx); err != nil {
		return "DATABASE_UNAVAILABLE"
	}
	var missing int64
	err := server.database.QueryRowContext(ctx, `
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
	err = server.database.QueryRowContext(ctx, `
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

func (server *Server) session(writer http.ResponseWriter, request *http.Request) {
	token := ""
	if cookie, err := request.Cookie(csrfCookieName); err == nil && validToken(cookie.Value) {
		token = cookie.Value
	}
	if token == "" {
		contents := make([]byte, 32)
		if _, err := rand.Read(contents); err != nil {
			writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "无法建立会话", map[string]any{})
			return
		}
		token = base64.RawURLEncoding.EncodeToString(contents)
	}
	http.SetCookie(writer, &http.Cookie{
		Name: csrfCookieName, Value: token, Path: "/", MaxAge: 86400,
		SameSite: http.SameSiteStrictMode, Secure: server.config.PublicOrigin.Scheme == "https",
	})
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{"csrfToken": token, "expiresAtMs": server.now().Add(24 * time.Hour).UnixMilli()},
	)
}

func validToken(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
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
	digestBytes := sha256.Sum256(append([]byte("postLaunch\x00"), canonical...))
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
`, key).
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
	created, err := server.launcher.Create(request.Context(), body)
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
INSERT INTO idempotency_records(operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms) VALUES('postLaunch',
?,
?,
?,
'{}',
?,
?,
?)
`, key, requestDigest, status, responseBody, now, now+int64(24*time.Hour/time.Millisecond)); err != nil {
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

func (server *Server) launchCapability(request *http.Request) string {
	cookie, err := request.Cookie("retrom_launch_" + request.PathValue("launchId"))
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (server *Server) launchConfig(writer http.ResponseWriter, request *http.Request) {
	configuration, err := server.launcher.Config(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
	)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
		return
	}
	writer.Header().Set("Vary", "Cookie")
	writeJSON(writer, http.StatusOK, configuration)
}

func (server *Server) launchDOSConfig(writer http.ResponseWriter, request *http.Request) {
	configuration, err := server.launcher.DOSConfig(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
	)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "DOS 启动配置不可用", map[string]any{})
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	http.ServeContent(writer, request, "game.conf", time.Unix(0, 0), strings.NewReader(configuration))
}

func (server *Server) launchGame(writer http.ResponseWriter, request *http.Request) {
	if rejectMultipleRanges(writer, request) {
		return
	}
	digest, err := server.launcher.ContentBlob(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
		request.PathValue("logicalName"),
	)
	if err != nil {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动内容不可用", map[string]any{})
		return
	}
	file, err := server.blobs.OpenDigest(digest)
	if err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	if _, err := file.Stat(); err != nil {
		writeError(writer, request, http.StatusServiceUnavailable, "CAS_UNAVAILABLE", "游戏内容不可用", map[string]any{})
		return
	}
	mediaType := mime.TypeByExtension(filepath.Ext(request.PathValue("logicalName")))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	writer.Header().Set("Content-Type", mediaType)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Vary", "Cookie")
	writer.Header().Set("ETag", `"sha256-`+digest+`"`)
	writer.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(writer, request, request.PathValue("logicalName"), time.Unix(0, 0), file)
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
	metadata, exists, err := server.saveService.GetPersistent(
		request.Context(),
		request.PathValue("launchId"),
		server.launchCapability(request),
	)
	if errors.Is(err, saves.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "启动会话不可用", map[string]any{})
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

func (server *Server) launchState(writer http.ResponseWriter, request *http.Request) {
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

//nolint:funlen // The dashboard aggregates documented counters in one consistent response snapshot.
func (server *Server) home(writer http.ResponseWriter, request *http.Request) {
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
AND g.status='PUBLISHED'
AND pi.enabled=1`,
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
AND pi.enabled=1`,
	).Scan(
		&activeDurationMS,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	recentGames := make([]map[string]any, 0, 6)
	gameRows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
max(ps.updated_at_ms),
sum(ps.active_duration_ms),
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,
 a.id
 LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
GROUP BY g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name
ORDER BY max(ps.updated_at_ms) DESC,
g.id LIMIT 6
`,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", gameRows.Close()) }()
	for gameRows.Next() {
		var gameID, title, platformID, platformName, instanceID, instanceName string
		var lastPlayedAtMS, durationMS int64
		var coverAssetID sql.NullString
		if err := gameRows.Scan(
			&gameID,
			&title,
			&platformID,
			&platformName,
			&instanceID,
			&instanceName,
			&lastPlayedAtMS,
			&durationMS,
			&coverAssetID,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		recentGames = append(
			recentGames,
			map[string]any{
				"gameId":           gameID,
				"title":            title,
				"platform":         map[string]any{"id": platformID, "name": platformName},
				"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
				"lastPlayedAtMs":   lastPlayedAtMS,
				"activeDurationMs": durationMS,
				"coverUrl":         gameCoverURL(coverAssetID),
			},
		)
	}
	if err := gameRows.Err(); err != nil {
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
s.active_duration_ms
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE s.deleted_at_ms IS NULL
AND g.status='PUBLISHED'
AND pi.enabled=1
ORDER BY s.created_at_ms DESC,
s.id DESC LIMIT 3
`,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", saveRows.Close()) }()
	for saveRows.Next() {
		var saveID, gameID, title, name string
		var createdAtMS, activeDurationMS int64
		if err := saveRows.Scan(&saveID, &gameID, &title, &name, &createdAtMS, &activeDurationMS); err != nil {
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
				"screenshotUrl":    saveStateScreenshotURL(saveID),
			},
		)
	}
	if err := saveRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"library":     map[string]any{"gameCount": gameCount, "saveStateCount": saveCount},
		"imports":     map[string]any{"reviewPendingCount": reviewCount},
		"play":        map[string]any{"activeDurationMs": activeDurationMS},
		"recentGames": recentGames, "recentSaves": recentSaves,
	})
}

// recentGames returns one row per visible game, ordered by the most recent
// durable play-session update. The page intentionally defaults to 50 rows so
// revisiting it does not turn into an unbounded history query.
func (server *Server) recentGames(writer http.ResponseWriter, request *http.Request) {
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "分页大小无效", map[string]any{})
			return
		}
		limit = parsed
	}
	rows, err := server.database.QueryContext(request.Context(), `
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
max(ps.updated_at_ms),
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
GROUP BY g.id,m.title,p.id,p.name,pi.id,pi.name
ORDER BY max(ps.updated_at_ms) DESC,g.id DESC
LIMIT ?
`, limit)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var gameID, title, platformID, platformName, instanceID, instanceName string
		var lastPlayedAtMS, activeDurationMS, sessionCount int64
		var coverAssetID sql.NullString
		if err := rows.Scan(&gameID, &title, &platformID, &platformName, &instanceID, &instanceName,
			&lastPlayedAtMS, &activeDurationMS, &sessionCount, &coverAssetID); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"gameId": gameID, "title": title,
			"platform":         map[string]any{"id": platformID, "name": platformName},
			"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
			"lastPlayedAtMs":   lastPlayedAtMS, "activeDurationMs": activeDurationMS,
			"sessionCount": sessionCount, "coverUrl": gameCoverURL(coverAssetID),
		})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "limit": limit})
}

func (server *Server) games(writer http.ResponseWriter, request *http.Request) {
	server.gameList(writer, request, false)
}

//nolint:funlen,gocyclo // Method dispatch and nullable detail projections stay at the route protocol boundary.
func (server *Server) game(writer http.ResponseWriter, request *http.Request) {
	gameID := request.PathValue("gameId")
	var title, description, developer, publisher, genre string
	var platformID, platformName, instanceID, instanceName, contentRevisionID string
	var players, releaseYear sql.NullInt64
	var coverAssetID sql.NullString
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
COALESCE((SELECT SUM(active_duration_ms)
FROM play_sessions ps
WHERE ps.game_id=g.id),
0)
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
`, gameID).
		Scan(&title, &description, &developer, &publisher, &genre, &players, &releaseYear,
			&platformID, &platformName, &instanceID, &instanceName, &contentRevisionID,
			&version, &updatedAtMS, &coverAssetID, &activeDurationMS,
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
AND deleted_at_ms IS NULL
`, gameID).Scan(&saveStateCount); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	saveRows, err := server.database.QueryContext(request.Context(), `
SELECT s.id,
s.name,
s.created_at_ms,
a.core_id,
c.name
FROM save_states s
JOIN core_artifacts a ON a.id=s.core_artifact_id
JOIN cores c ON c.id=a.core_id
WHERE s.game_id=?
AND s.deleted_at_ms IS NULL
ORDER BY s.created_at_ms DESC,
s.id DESC
LIMIT 8
`, gameID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", saveRows.Close()) }()
	saveStates := make([]map[string]any, 0)
	for saveRows.Next() {
		var saveID, saveName, coreID, coreName string
		var createdAtMS int64
		if err := saveRows.Scan(&saveID, &saveName, &createdAtMS, &coreID, &coreName); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		saveStates = append(saveStates, map[string]any{
			"saveStateId": saveID, "name": saveName, "createdAtMs": createdAtMS,
			"screenshotUrl": saveStateScreenshotURL(saveID),
			"core":          map[string]any{"id": coreID, "name": coreName},
		})
	}
	if err := saveRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"gameId": gameID, "title": title, "description": description, "developer": developer, "publisher": publisher,
		"genre": genre, "players": nullableInteger(players), "releaseYear": nullableInteger(releaseYear),
		"platform":                 map[string]any{"id": platformID, "name": platformName},
		"platformInstance":         map[string]any{"id": instanceID, "name": instanceName},
		"currentContentRevisionId": contentRevisionID, "version": version, "updatedAtMs": updatedAtMS,
		"coverUrl": gameCoverURL(coverAssetID), "activeDurationMs": activeDurationMS, "coreOptions": coreOptions,
		"dosEntries": dosEntries, "defaultDosEntry": nullableString(defaultDOSEntry),
		"saveStateCount": saveStateCount, "saveStates": saveStates,
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

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (server *Server) gameList(writer http.ResponseWriter, request *http.Request, includeDeleted bool) {
	query := `
SELECT g.id,
 m.title,
 p.id,
 p.name,
 pi.id,
 pi.name,
 g.status,
 g.version,
 g.updated_at_ms,
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
`
	conditions := gameListVisibilityConditions(includeDeleted)
	arguments := make([]any, 0)
	values := request.URL.Query()
	if !includeDeleted || values.Get("status") == "PUBLISHED" {
		conditions = append(conditions, "g.status='PUBLISHED'")
	} else if values.Get("status") == "DELETED" {
		conditions = append(conditions, "g.status='DELETED'")
	} else if status := values.Get("status"); status != "" && status != "ALL" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏状态筛选无效", map[string]any{})
		return
	}
	normalizedQ := strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " "))
	if normalizedQ != "" {
		conditions = append(conditions, "instr(g.search_text,?)>0")
		arguments = append(arguments, normalizedQ)
	}
	if platformID := values.Get("platformId"); platformID != "" {
		conditions = append(conditions, "p.id=?")
		arguments = append(arguments, platformID)
	}
	if instanceID := values.Get("platformInstanceId"); instanceID != "" {
		conditions = append(conditions, "pi.id=?")
		arguments = append(arguments, instanceID)
	}
	operationID := "getGames"
	if includeDeleted {
		operationID = "getAdminGames"
	}
	filterDigest := cursor.FilterDigest(
		map[string]any{
			"q":                  normalizedQ,
			"platformId":         values.Get("platformId"),
			"platformInstanceId": values.Get("platformInstanceId"),
			"status":             values.Get("status"),
		},
	)
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, operationID, filterDigest, "TITLE_ASC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		conditions = append(conditions, "(m.title>? OR (m.title=? AND g.id>?))")
		arguments = append(arguments, payload.SortValues[0], payload.SortValues[0], payload.ID)
	}
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	query = queryWithConditions(query, conditions, " ORDER BY m.title,g.id LIMIT ?")
	arguments = append(arguments, limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0, limit+1)
	for rows.Next() {
		var id, title, platformID, platformName, instanceID, instanceName, status string
		var version, updatedAtMS int64
		var coverAssetID sql.NullString
		if err := rows.Scan(
			&id,
			&title,
			&platformID,
			&platformName,
			&instanceID,
			&instanceName,
			&status,
			&version,
			&updatedAtMS,
			&coverAssetID,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"gameId": id, "title": title, "platform": map[string]any{"id": platformID, "name": platformName},
			"platformInstance": map[string]any{"id": instanceID, "name": instanceName}, "status": status,
			"version": version, "updatedAtMs": updatedAtMS, "coverUrl": gameCoverURL(coverAssetID),
		})
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if len(items) > limit {
		last := items[limit-1]
		lastTitle, titleOK := last["title"].(string)
		lastID, idOK := last["gameId"].(string)
		if !titleOK || !idOK {
			writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "游戏分页投影无效", map[string]any{})
			return
		}
		items = items[:limit]
		token, err := server.cursors.Encode(
			cursor.Payload{
				OperationID:  operationID,
				FilterDigest: filterDigest,
				SortCode:     "TITLE_ASC",
				SortValues:   []string{lastTitle},
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

type saveListFilters struct {
	Conditions   []string
	Arguments    []any
	NormalizedQ  string
	Availability string
	Digest       string
}

func parseSaveListFilters(values url.Values) (saveListFilters, error) {
	filters := saveListFilters{
		Conditions:   []string{"s.deleted_at_ms IS NULL", "pi.enabled=1"},
		Arguments:    make([]any, 0),
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
		"q":                  filters.NormalizedQ,
		"gameId":             values.Get("gameId"),
		"platformId":         values.Get("platformId"),
		"platformInstanceId": values.Get("platformInstanceId"),
		"coreId":             values.Get("coreId"),
		"availability":       filters.Availability,
	})
	return filters, nil
}

//nolint:funlen // Query projection stays contiguous with pagination assembly.
func (server *Server) saves(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	filters, err := parseSaveListFilters(values)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "存档可用性筛选无效", map[string]any{})
		return
	}
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getSaves", filters.Digest, "CREATED_DESC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		createdAt, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
		if err != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		filters.Conditions = append(filters.Conditions, "(s.created_at_ms<? OR (s.created_at_ms=? AND s.id<?))")
		filters.Arguments = append(filters.Arguments, createdAt, createdAt, payload.ID)
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
pi.platform_id,
pi.id,
pi.name
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN core_artifacts a ON a.id=s.core_artifact_id
JOIN cores c ON c.id=a.core_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
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
		var id, gameID, gameTitle, name, coreID, coreName, gameStatus, platformID, instanceID, instanceName string
		var version, createdAtMS, activeDurationMS int64
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
			&instanceID,
			&instanceName,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"saveStateId": id, "gameId": gameID, "gameTitle": gameTitle,
			"name": name, "version": version, "createdAtMs": createdAtMS,
			"activeDurationMs": activeDurationMS, "screenshotUrl": saveStateScreenshotURL(id),
			"core": map[string]any{
				"id":   coreID,
				"name": coreName,
			}, "platformId": platformID, "platformInstance": map[string]any{"id": instanceID, "name": instanceName},
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
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (server *Server) patchSave(writer http.ResponseWriter, request *http.Request) {
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
AND version=?
AND deleted_at_ms IS NULL
`,
		body.Name,
		now,
		request.PathValue("saveStateId"),
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
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
AND version=?
AND deleted_at_ms IS NULL
`,
		now,
		now,
		request.PathValue("saveStateId"),
		expected,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
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

func (server *Server) platformInstances(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	conditions := []string{"pi.deleted_at_ms IS NULL"}
	arguments := make([]any, 0, 2)
	if value := values.Get("platformId"); value != "" {
		conditions = append(conditions, "pi.platform_id=?")
		arguments = append(arguments, value)
	}
	if value := values.Get("enabled"); value != "" {
		if value != "true" && value != "false" {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "目录启用状态无效", map[string]any{})
			return
		}
		conditions = append(conditions, "pi.enabled=?")
		arguments = append(arguments, map[string]int{"true": 1, "false": 0}[value])
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
		var id, platformID, platformName, coreID, coreName, name, slug, description string
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
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "platformId": platformID, "platformName": platformName, "defaultCoreId": coreID,
			"defaultCoreName": coreName, "name": name, "slug": slug, "description": description,
			"sortOrder": sortOrder, "enabled": enabled == 1, "version": version, "updatedAtMs": updatedAtMS,
			"gameCount": gameCount,
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
j.version
FROM dat_versions d
JOIN cores c ON c.id=d.core_id
LEFT JOIN dat_import_jobs dj ON dj.dat_version_id=d.id
LEFT JOIN jobs j ON j.id=dj.job_id
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
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "coreId": coreID, "coreName": coreName, "coreArtifactId": artifactID, "source": source,
			"compatibilityStatus": compatibility, "parseStatus": status, "active": active == 1,
			"machineCount": nullableInteger(machineCount), "romEntryCount": nullableInteger(romCount),
			"diskEntryCount": nullableInteger(diskCount), "biosSetCount": nullableInteger(biosCount),
			"version": version, "updatedAtMs": updatedAtMS, "jobId": nullableString(jobID),
			"jobState": nullableString(jobState), "jobVersion": nullableInteger(jobVersion),
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

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (server *Server) bios(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	conditions := []string{"b.enabled=1"}
	arguments := make([]any, 0, 8)
	if value := strings.TrimSpace(values.Get("q")); value != "" {
		conditions = append(conditions, "(instr(lower(b.logical_name),lower(?))>0 OR instr(lower(c.name),lower(?))>0)")
		arguments = append(arguments, value, value)
	}
	filters := map[string]string{
		"coreId": "b.core_id", "coreArtifactId": "b.core_artifact_id", "platformId": "a.platform_id",
	}
	for name, column := range filters {
		if value := values.Get(name); value != "" {
			conditions = append(conditions, column+"=?")
			arguments = append(arguments, value)
		}
	}
	scope := values.Get("scope")
	if scope == "" {
		scope = "REQUIRED_BY_LIBRARY"
	}
	if scope != "FULL_CATALOG" && scope != "REQUIRED_BY_LIBRARY" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "BIOS 需求范围无效", map[string]any{})
		return
	}
	if scope == "REQUIRED_BY_LIBRARY" {
		conditions = append(
			conditions,
			`EXISTS(
SELECT 1 FROM game_variants v
JOIN game_variant_revisions r ON r.id=v.current_revision_id
JOIN games g ON g.id=v.game_id
WHERE r.core_artifact_id=b.core_artifact_id AND g.status='PUBLISHED'
)`,
		)
	}
	if status := values.Get("status"); status != "" {
		if status != "MATCHED" && status != "MISSING" && status != "HASH_WARNING" && status != "MISSING_ENTRY" &&
			status != "OPTIONAL_MISSING" {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "BIOS 状态无效", map[string]any{})
			return
		}
		conditions = append(
			conditions,
			"COALESCE(i.status,CASE WHEN b.requirement_mode='OPTIONAL' THEN 'OPTIONAL_MISSING' ELSE 'MISSING' END)=?",
		)
		arguments = append(arguments, status)
	}
	query := queryWithConditions(
		`
SELECT b.id,
b.core_id,
c.name,
b.core_artifact_id,
b.logical_name,
b.requirement_mode,
b.condition_code,
b.md5,
b.enabled,
b.version,
COALESCE(i.status,
CASE WHEN b.requirement_mode='OPTIONAL' THEN 'OPTIONAL_MISSING' ELSE 'MISSING' END),
i.id,
i.md5,
i.sha1,
i.sha256,
i.validated_requirement_version,
i.created_at_ms
FROM bios_requirements b
JOIN cores c ON c.id=b.core_id
JOIN core_artifacts a ON a.id=b.core_artifact_id
LEFT JOIN bios_installations i ON i.requirement_id=b.id
AND i.is_active=1
`,
		conditions,
		` ORDER BY c.name,b.logical_name,b.id LIMIT 100`,
	)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, coreID, coreName, artifactID, logicalName, mode, status string
		var condition, expectedMD5, installationID, installedMD5, installedSHA1, installedSHA256 sql.NullString
		var validatedVersion, installedAt sql.NullInt64
		var enabled int
		var version int64
		if err := rows.Scan(
			&id,
			&coreID,
			&coreName,
			&artifactID,
			&logicalName,
			&mode,
			&condition,
			&expectedMD5,
			&enabled,
			&version,
			&status,
			&installationID,
			&installedMD5,
			&installedSHA1,
			&installedSHA256,
			&validatedVersion,
			&installedAt,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(
			items,
			map[string]any{
				"id":              id,
				"coreId":          coreID,
				"coreName":        coreName,
				"coreArtifactId":  artifactID,
				"logicalName":     logicalName,
				"requirementMode": mode,
				"conditionCode":   nullableString(condition),
				"expectedMd5":     nullableString(expectedMD5),
				"enabled":         enabled == 1,
				"version":         version,
				"status":          status,
				"activeInstallation": nullableBIOSInstallation(
					installationID,
					installedMD5,
					installedSHA1,
					installedSHA256,
					validatedVersion,
					installedAt,
				),
			},
		)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
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

func (server *Server) importSummary(writer http.ResponseWriter, request *http.Request) {
	counts := map[string]int64{"running": 0, "reviewPending": 0, "completed": 0, "failed": 0}
	rows, err := server.database.QueryContext(
		request.Context(),
		"SELECT state,count(*) FROM import_jobs GROUP BY state",
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		switch state {
		case "QUEUED", "RUNNING", "CANCEL_REQUESTED":
			counts["running"] += count
		case "REVIEW_PENDING":
			counts["reviewPending"] += count
		case "COMPLETED":
			counts["completed"] += count
		case "PARTIAL_FAILURE", "FAILED", "CANCELLED":
			counts["failed"] += count
		}
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, counts)
}

func (server *Server) imports(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	conditions := []string{"1=1"}
	arguments := make([]any, 0, 4)
	if value := strings.TrimSpace(values.Get("q")); value != "" {
		conditions = append(conditions, "(instr(lower(i.id),lower(?))>0 OR instr(lower(pi.name),lower(?))>0)")
		arguments = append(arguments, value, value)
	}
	if value := values.Get("state"); value != "" {
		conditions = append(conditions, "i.state=?")
		arguments = append(arguments, value)
	}
	if value := values.Get("platformInstanceId"); value != "" {
		conditions = append(conditions, "i.target_platform_instance_id=?")
		arguments = append(arguments, value)
	}
	query := queryWithConditions(
		`
SELECT i.id,
i.state,
pi.name,
i.metadata_provider,
i.total_item_count,
i.review_pending_item_count,
i.failed_item_count,
i.version,
i.created_at_ms,
i.updated_at_ms
FROM import_jobs i
JOIN platform_instances pi ON pi.id=i.target_platform_instance_id
`,
		conditions,
		` ORDER BY i.updated_at_ms DESC,i.id DESC LIMIT 100`,
	)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, state, platformName, provider string
		var total, pending, failed, version, createdAtMS, updatedAtMS int64
		if err := rows.Scan(
			&id,
			&state,
			&platformName,
			&provider,
			&total,
			&pending,
			&failed,
			&version,
			&createdAtMS,
			&updatedAtMS,
		); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(
			items,
			map[string]any{
				"id":                     id,
				"state":                  state,
				"platformInstanceName":   platformName,
				"metadataProvider":       provider,
				"totalItemCount":         total,
				"reviewPendingItemCount": pending,
				"failedItemCount":        failed,
				"version":                version,
				"createdAtMs":            createdAtMS,
				"updatedAtMs":            updatedAtMS,
			},
		)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nil})
}

func (server *Server) createImport(writer http.ResponseWriter, request *http.Request) {
	var body libraryimport.CreateRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "导入配置无效", map[string]any{})
		return
	}
	created, err := server.importer.Create(request.Context(), body)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "IMPORT_INPUT_INVALID", "上传或目标目录不可用于导入", map[string]any{})
		return
	}
	writeJSON(writer, http.StatusAccepted, created)
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (server *Server) reviews(writer http.ResponseWriter, request *http.Request) {
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
 FROM import_item_source_files source_file
 JOIN blobs b ON b.id=source_file.blob_id
 WHERE source_file.import_item_id=i.id),
(SELECT b.md5
 FROM import_item_source_files source_file
 JOIN blobs b ON b.id=source_file.blob_id
 WHERE source_file.import_item_id=i.id
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
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
	LEFT
JOIN import_item_core_validations v ON v.id=COALESCE(d.selected_validation_id,
(SELECT candidate.id
FROM import_item_core_validations candidate
WHERE candidate.import_item_id=i.id
AND candidate.target_platform_instance_id=d.target_platform_instance_id
ORDER BY candidate.created_at_ms DESC,
candidate.id DESC LIMIT 1))
WHERE i.state='REVIEW_PENDING'
`
	arguments := []any{}
	values := request.URL.Query()
	allowed := map[string]struct{}{
		"q":                  {},
		"importJobId":        {},
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
	normalizedQ := strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " "))
	if len([]rune(normalizedQ)) > 200 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "待审核搜索词过长", map[string]any{})
		return
	}
	if normalizedQ != "" {
		query += " AND instr(i.search_text,?)>0"
		arguments = append(arguments, normalizedQ)
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
			"q":                  normalizedQ,
			"importJobId":        values.Get("importJobId"),
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
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
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
				"coverUrl":             reviewAssetURL(coverAssetID),
				"updatedAtMs":          updatedAtMS,
			},
		)
	}
	if err := rows.Err(); err != nil {
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

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (server *Server) review(writer http.ResponseWriter, request *http.Request) {
	var itemID, importJobID, metadata, platformID, platformName, sourceManifest string
	var validationID, validationStatus, compatibilityCode, dependencySnapshot sql.NullString
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
v.id,
v.status,
v.compatibility_code,
v.dependency_snapshot_json,
i.source_manifest_json,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.cover_uploaded_asset_id,
d.background_candidate_asset_id,
d.default_dos_entry
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
LEFT
JOIN import_item_core_validations v ON v.id=COALESCE(d.selected_validation_id,
(
  SELECT candidate.id
FROM import_item_core_validations candidate
WHERE candidate.import_item_id=i.id
AND candidate.target_platform_instance_id=d.target_platform_instance_id
ORDER BY candidate.created_at_ms DESC,
candidate.id DESC LIMIT 1
))
WHERE i.id=?
AND i.state='REVIEW_PENDING'
`, request.PathValue("importItemId")).
		Scan(
			&itemID,
			&importJobID,
			&metadata,
			&version,
			&updatedAtMS,
			&platformID,
			&platformName,
			&validationID,
			&validationStatus,
			&compatibilityCode,
			&dependencySnapshot,
			&sourceManifest,
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
	var metadataValue, sourceValue, dependencyValue any
	_ = json.Unmarshal([]byte(metadata), &metadataValue)
	_ = json.Unmarshal([]byte(sourceManifest), &sourceValue)
	if files, ok := sourceValue.([]any); ok {
		sourceValue = map[string]any{"files": files}
	}
	if dependencySnapshot.Valid {
		_ = json.Unmarshal([]byte(dependencySnapshot.String), &dependencyValue)
	}
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
	sourceFiles, err := server.reviewSourceFiles(request, itemID)
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
	validation := any(nil)
	if validationID.Valid {
		validation = map[string]any{
			"id":                 validationID.String,
			"status":             validationStatus.String,
			"compatibilityCode":  compatibilityCode.String,
			"dependencySnapshot": dependencyValue,
		}
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(writer, http.StatusOK, map[string]any{
		"itemId": itemID, "importJobId": importJobID, "version": version, "updatedAtMs": updatedAtMS,
		"platformInstance": map[string]any{
			"id":   platformID,
			"name": platformName,
		}, "metadata": metadataValue, "sourceManifest": sourceValue,
		"validation": validation, "candidates": candidates, "scrapeRuns": scrapeRuns,
		"uploadedAssets": uploadedAssets, "sourceFiles": sourceFiles,
		"selectedCandidateId": nullableString(selectedCandidateID),
		"defaultDosEntry":     nullableString(defaultDOSEntry),
		"selectedAssets": map[string]any{
			"coverCandidateAssetId":       nullableString(coverID),
			"coverUploadedAssetId":        nullableString(uploadedCoverID),
			"backgroundCandidateAssetId":  nullableString(backgroundID),
			"screenshotCandidateAssetIds": screenshotIDs,
		}, "dosEntries": dosEntries,
	})
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

func (server *Server) reviewSourceFiles(request *http.Request, itemID string) ([]map[string]any, error) {
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
FROM import_item_source_files s
JOIN upload_files f ON f.id=s.upload_file_id
JOIN blobs b ON b.id=f.final_blob_id
WHERE s.import_item_id=?
GROUP BY f.id,f.relative_path,b.size_bytes,b.sha256,b.md5,b.crc32
ORDER BY min(s.sort_order),f.relative_path,f.id
`, itemID)
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
		if record.archiveBlobID.Valid {
			entries, err = server.reviewArchiveEntries(request.Context(), record.archiveBlobID.String)
			if err != nil {
				return nil, err
			}
		} else {
			entries = make([]map[string]any, 0)
		}
		result = append(result, map[string]any{
			"uploadFileId": record.id, "name": record.name, "sizeBytes": record.size,
			"sha256": record.sha256, "md5": record.md5, "crc32": record.crc32,
			"archive": record.archive == 1, "archiveEntries": entries,
		})
	}
	return result, nil
}

func (server *Server) reviewArchiveEntries(ctx context.Context, archiveBlobID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT original_relative_path,uncompressed_size_bytes,crc32
FROM archive_entries
WHERE archive_blob_id=?
ORDER BY ordinal
`, archiveBlobID)
	if err != nil {
		return nil, fmt.Errorf("query review archive entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0)
	for rows.Next() {
		var name, crc32 string
		var size int64
		if err := rows.Scan(&name, &size, &crc32); err != nil {
			return nil, fmt.Errorf("scan review archive entry: %w", err)
		}
		entries = append(entries, map[string]any{"name": name, "sizeBytes": size, "crc32": crc32})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review archive entries: %w", err)
	}
	return entries, nil
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
		Reason *string `json:"reason"`
	}
	if err := decodeJSON(writer, request, &body, 8<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "审核决定无效", map[string]any{})
		return
	}
	approved, err := server.importer.ApproveWithReason(
		request.Context(),
		request.PathValue("importItemId"),
		version,
		body.Reason,
	)
	if err != nil {
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
) LIMIT 1
`, request.PathValue("assetId"), request.PathValue("assetId")).Scan(&digest, &mediaType)
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
