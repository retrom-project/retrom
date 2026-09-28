package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/config"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/service/runtimesession"
)

const runtimeCookieName = "retrom_runtime"

type (
	runtimeSessionKey struct{}
	runtimeGrantsKey  struct{}
)

func runtimeSessionFromRequest(request *http.Request) (runtimesession.Session, bool) {
	session, ok := request.Context().Value(runtimeSessionKey{}).(runtimesession.Session)
	return session, ok
}

func (server *Server) setRuntimeCookie(writer http.ResponseWriter, session runtimesession.Session) error {
	domain, err := config.RuntimeCookieDomain(server.config.PublicOrigin, server.config.RPGRuntimeOriginTemplate)
	if err != nil {
		return fmt.Errorf("runtime cookie domain: %w", err)
	}
	remaining := (session.ExpiresAtMS - server.now().UnixMilli() + 999) / 1000
	http.SetCookie(writer, &http.Cookie{
		Name: runtimeCookieName, Value: session.Token, Path: "/", Domain: domain,
		Expires: time.UnixMilli(session.ExpiresAtMS), MaxAge: int(remaining), HttpOnly: true,
		Secure: server.config.PublicOrigin.Scheme == "https", SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (server *Server) ensureRuntimeSession(writer http.ResponseWriter, request *http.Request) bool {
	principal, ok := authn.PrincipalFromContext(request.Context())
	if !ok {
		writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要登录", map[string]any{})
		return false
	}
	session, err := server.runtimeSessionForPrincipal(request, principal)
	if err != nil {
		server.runtimeSessionError(writer, request, err)
		return false
	}
	if err := server.setRuntimeCookie(writer, session); err != nil {
		server.databaseError(writer, request, err)
		return false
	}
	return true
}

func (server *Server) runtimeSessionError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, runtimesession.ErrCredential) {
		writeError(writer, request, http.StatusUnauthorized, "LAUNCH_CREDENTIAL_INVALID", "运行会话不可用", map[string]any{})
		return
	}
	server.databaseError(writer, request, err)
}

func (server *Server) authenticateRuntimeRequest(
	writer http.ResponseWriter, request *http.Request,
) (*http.Request, bool) {
	server.clearLegacyRuntimeCookies(writer, request)
	session, err := server.readRuntimeSession(request)
	if err != nil {
		server.runtimeSessionError(writer, request, err)
		return request, false
	}
	// Reuse the same value, including concurrent requests; expiry is server-authoritative.
	if session.Refreshed {
		if err := server.setRuntimeCookie(writer, session); err != nil {
			server.databaseError(writer, request, err)
			return request, false
		}
	}
	ctx := context.WithValue(request.Context(), runtimeSessionKey{}, session)
	if tail, ok := strings.CutPrefix(request.URL.Path, "/runtime/launches/"); ok {
		id, _, _ := strings.Cut(tail, "/")
		authorize := server.playDeps.RuntimeSessions.AuthorizeRun
		if strings.HasSuffix(request.URL.Path, "/finish") {
			authorize = server.playDeps.RuntimeSessions.AuthorizePreviewFinish
		}
		if err := authorize(ctx, session, id); err != nil {
			server.runtimeSessionError(writer, request, err)
			return request, false
		}
	}
	if strings.HasPrefix(request.URL.Path, "/runtime/content/") {
		ids, readErr := server.playDeps.RuntimeSessions.Runs(ctx, session, "")
		if readErr != nil {
			server.databaseError(writer, request, readErr)
			return request, false
		}
		grants := make([]runtimeContentGrant, 0, len(ids))
		for _, id := range ids {
			parsed, parseErr := uuid.Parse(id)
			if parseErr != nil {
				continue
			}
			grants = append(grants, runtimeContentGrant{
				LaunchID:   id,
				Capability: retromruntime.EncodeCapability(server.contentDeps.Credentials.Capability(parsed)),
			})
		}
		ctx = context.WithValue(ctx, runtimeGrantsKey{}, grants)
	}
	return request.WithContext(ctx), true
}

func (server *Server) renewRuntimeSession(writer http.ResponseWriter, request *http.Request) {
	// Synchronize browser expiry even if another request already renewed the server lease.
	session, ok := runtimeSessionFromRequest(request)
	if !ok {
		server.runtimeSessionError(writer, request, runtimesession.ErrCredential)
		return
	}
	if !session.Refreshed {
		if err := server.setRuntimeCookie(writer, session); err != nil {
			server.databaseError(writer, request, err)
			return
		}
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) readRuntimeSession(request *http.Request) (runtimesession.Session, error) {
	cookies := request.CookiesNamed(runtimeCookieName)
	if len(cookies) != 1 || server.playDeps.RuntimeSessions == nil {
		return runtimesession.Session{}, runtimesession.ErrCredential
	}
	session, err := server.playDeps.RuntimeSessions.Authenticate(request.Context(), cookies[0].Value)
	if err != nil {
		return session, fmt.Errorf("authenticate runtime session: %w", err)
	}
	return session, nil
}
