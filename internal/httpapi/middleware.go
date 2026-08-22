package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/config"
)

// One boundary applies readiness, origin, authentication, role, and CSRF in fixed order.
func (server *Server) baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.serveBaseMiddleware(next, writer, request)
	})
}

func (server *Server) serveBaseMiddleware(next http.Handler, writer http.ResponseWriter, request *http.Request) {
	requestID, err := uuid.NewV7()
	if err != nil {
		http.Error(writer, "request id unavailable", http.StatusInternalServerError)
		return
	}
	requestContext := context.WithValue(request.Context(), requestIDKey, requestID.String())
	request = request.WithContext(requestContext)
	setBaseResponseHeaders(writer, requestID.String())
	if !server.requestReady(requestContext, writer, request) || !server.validRequest(writer, request) {
		return
	}
	if publicHTTPRoute(request) || launchHTTPRoute(request.URL.Path) {
		next.ServeHTTP(writer, request)
		return
	}
	session, ok := server.authenticateRequest(writer, request)
	if !ok || !server.authorizeRequest(writer, request, session) {
		return
	}
	request = request.WithContext(authn.WithPrincipal(requestContext, session.Principal))
	next.ServeHTTP(writer, request)
}

func (server *Server) validRequest(writer http.ResponseWriter, request *http.Request) bool {
	if err := validateQuery(request); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "查询参数无效", map[string]any{})
		return false
	}
	protectedMode := server.config.Mode == config.ModeRelease || server.config.Mode == config.ModeTest
	if protectedMode && unsafeMethod(request.Method) && !server.validRequestOrigin(request) {
		writeError(writer, request, http.StatusForbidden, "REQUEST_ORIGIN_INVALID", "请求来源无效", map[string]any{})
		return false
	}
	return true
}

func (server *Server) authenticateRequest(
	writer http.ResponseWriter,
	request *http.Request,
) (accounts.Session, bool) {
	if server.accounts != nil {
		contextView, err := server.accounts.Context(request.Context(), server.authCookieToken(request))
		if err != nil {
			server.databaseError(writer, request, err)
			return accounts.Session{}, false
		}
		if contextView.InstanceState == "INITIALIZATION_REQUIRED" {
			writeError(writer, request, http.StatusPreconditionRequired, "INITIALIZATION_REQUIRED", "实例尚未初始化", map[string]any{})
			return accounts.Session{}, false
		}
	}
	if server.authenticator == nil {
		writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要登录", map[string]any{})
		return accounts.Session{}, false
	}
	session, err := server.authenticator.Authenticate(request.Context(), server.authCookieToken(request))
	if errors.Is(err, accounts.ErrAuthenticationNeeded) {
		server.clearAuthCookies(writer)
		writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要登录", map[string]any{})
		return accounts.Session{}, false
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return accounts.Session{}, false
	}
	return session, true
}

func (server *Server) authorizeRequest(
	writer http.ResponseWriter,
	request *http.Request,
	session accounts.Session,
) bool {
	if strings.HasPrefix(request.URL.Path, "/api/v1/admin/") && session.Principal.Role != "ADMIN" {
		writeError(writer, request, http.StatusForbidden, "ADMIN_REQUIRED", "需要管理员权限", map[string]any{})
		return false
	}
	protectedMode := server.config.Mode == config.ModeRelease || server.config.Mode == config.ModeTest
	if protectedMode && unsafeMethod(request.Method) &&
		!accounts.MatchesCSRF(session.CookieToken, request.Header.Get("X-Retrom-Csrf")) {
		writeError(writer, request, http.StatusForbidden, "CSRF_VALIDATION_FAILED", "请求验证失败", map[string]any{})
		return false
	}
	return true
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
	"GET /api/v1/admin/tags":                               {"q", "status", "sort", "cursor", "limit"},
	"GET /api/v1/admin/platform-instances":                 {"platformId", "enabled", "sort", "cursor", "limit"},
	"GET /api/v1/admin/platform-instances/recommendations": {},
	"GET /api/v1/admin/bios": {
		"q", "platformId", "coreId", "coreArtifactId", "scope", "status", "quick", "cursor", "limit",
	},
	"GET /api/v1/admin/server-import-roots": {},
	"GET /api/v1/admin/server-imports":      {"kind", "state", "cursor", "limit"},
	"GET /api/v1/admin/pegasus-imports":     {"state", "cursor", "limit"},
	"GET /api/v1/admin/users":               {"q", "role", "status", "sort", "cursor", "limit"},
	"GET /api/v1/admin/invitations":         {"state", "cursor", "limit"},
}

func reviewBulkQueryParameterNames(method, path string) []string {
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/review-bulk-approvals/") &&
		strings.HasSuffix(path, "/items") {
		return []string{"outcome", "cursor", "limit"}
	}
	return nil
}

// The lexical query parser handles independent escaping and separator states.
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
	return resourceQueryParameterNames(request.Method, path)
}

func resourceQueryParameterNames(method, path string) []string {
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/users/") &&
		strings.HasSuffix(path, "/password-reset-links") {
		return []string{"state", "cursor", "limit"}
	}
	if names := reviewBulkQueryParameterNames(method, path); names != nil {
		return names
	}
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/server-import-roots/") &&
		strings.HasSuffix(path, "/directories") {
		return []string{"path", "cursor", "limit"}
	}
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/server-imports/") {
		return serverImportQueryParameterNames(path)
	}
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/pegasus-imports/") {
		return pegasusImportQueryParameterNames(path)
	}
	return nil
}

func serverImportQueryParameterNames(path string) []string {
	if strings.Contains(path, "/bios-items/") && strings.HasSuffix(path, "/candidates") {
		return []string{"cursor", "limit"}
	}
	return []string{"q", "outcome", "matchMethod", "cursor", "limit"}
}

func pegasusImportQueryParameterNames(path string) []string {
	if strings.HasSuffix(path, "/collections") {
		return []string{"cursor", "limit"}
	}
	if strings.HasSuffix(path, "/items") {
		return []string{"q", "outcome", "warning", "collectionId", "cursor", "limit"}
	}
	return []string{}
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
AND d.source='BUILTIN'
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
AND active.source='BUILTIN'
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
