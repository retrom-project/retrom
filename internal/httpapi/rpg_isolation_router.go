package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/rpgmaker/isolation"
)

const rpgRuntimeCookieName = "retrom_rpg_runtime"

const rpgRuntimePermissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=(), usb=(), " +
	"serial=(), bluetooth=(), midi=(), clipboard-read=(), clipboard-write=()"

func (server *Server) routeByRuntimeHost(application http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		access, runtimeHost := server.rpgIsolation.ResolveHost(request.Host)
		if !runtimeHost {
			if server.rpgIsolation.IsRuntimeHostCandidate(request.Host) ||
				strings.HasPrefix(request.URL.Path, "/__retrom/") {
				http.NotFound(writer, request)
				return
			}
			application.ServeHTTP(writer, request)
			return
		}
		server.serveRPGRuntimeHost(writer, request, access)
	})
}

func (server *Server) serveRPGRuntimeHost(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	requestID, err := uuid.NewV7()
	if err != nil {
		http.Error(writer, "request id unavailable", http.StatusInternalServerError)
		return
	}
	requestContext := context.WithValue(request.Context(), requestIDKey, requestID.String())
	request = request.WithContext(requestContext)
	setRPGRuntimeResponseHeaders(writer, requestID.String())
	if request.URL.RawQuery != "" || request.URL.Fragment != "" {
		http.NotFound(writer, request)
		return
	}
	if !server.requestReady(requestContext, writer, request) {
		return
	}
	server.serveRPGRuntimeRoute(writer, request, access)
}

func (server *Server) serveRPGRuntimeRoute(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	switch request.Method {
	case http.MethodGet:
		server.serveRPGRuntimeGet(writer, request, access)
	case http.MethodPost:
		server.serveRPGRuntimePost(writer, request, access)
	default:
		http.NotFound(writer, request)
	}
}

func (server *Server) serveRPGRuntimeGet(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	switch {
	case request.URL.Path == "/__retrom/bootstrap":
		server.rpgBootstrapPage(writer, request, access)
	case request.URL.Path == "/__retrom/entry":
		server.rpgRuntimeEntry(writer, request, access)
	case request.URL.Path == "/__retrom/bridge.js":
		server.rpgRuntimeBridge(writer, request, access)
	case strings.HasPrefix(request.URL.Path, "/__retrom/project/"):
		server.rpgRuntimeProject(writer, request, access)
	case request.URL.Path == "/__retrom/restore-payload":
		server.rpgRuntimeRestorePayload(writer, request, access)
	default:
		http.NotFound(writer, request)
	}
}

func (server *Server) serveRPGRuntimePost(
	writer http.ResponseWriter,
	request *http.Request,
	access isolation.Access,
) {
	switch request.URL.Path {
	case "/__retrom/bootstrap":
		server.rpgBootstrapConsume(writer, request, access)
	case "/__retrom/cleanup":
		server.rpgRuntimeCleanup(writer, request, access)
	default:
		http.NotFound(writer, request)
	}
}

func setRPGRuntimeResponseHeaders(writer http.ResponseWriter, requestID string) {
	setBaseResponseHeaders(writer, requestID)
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("Permissions-Policy", rpgRuntimePermissionsPolicy)
}

func rpgRuntimeNonce() (string, error) {
	contents := make([]byte, 32)
	if _, err := rand.Read(contents); err != nil {
		return "", fmt.Errorf("generate RPG runtime nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(contents), nil
}

func (server *Server) authenticateRPGRuntime(
	request *http.Request,
	access isolation.Access,
) (isolation.Access, error) {
	cookies := request.CookiesNamed(rpgRuntimeCookieName)
	if len(cookies) != 1 || cookies[0].Value == "" {
		return isolation.Access{}, isolation.ErrCredential
	}
	authorized, err := server.rpgIsolation.Authenticate(
		request.Context(), access.LaunchID, access.Origin, cookies[0].Value,
	)
	if err != nil {
		return isolation.Access{}, fmt.Errorf("authenticate RPG runtime: %w", err)
	}
	return authorized, nil
}

func validRPGRuntimeWrite(request *http.Request, origin string) bool {
	origins := request.Header.Values("Origin")
	return len(origins) == 1 && origins[0] == origin &&
		(request.Header.Get("Sec-Fetch-Site") == "" || request.Header.Get("Sec-Fetch-Site") == "same-origin")
}
