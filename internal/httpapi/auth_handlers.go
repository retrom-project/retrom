package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"retrom/internal/accounts"
	"retrom/internal/authn"
)

type newAccountCredentialRequest struct {
	Username             string `json:"username"`
	DisplayName          string `json:"displayName"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"passwordConfirmation"`
}

func decodeNewAccountCredential(
	writer http.ResponseWriter,
	request *http.Request,
	target any,
	message string,
) bool {
	if err := decodeJSON(writer, request, target, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", message, map[string]any{})
		return false
	}
	return true
}

func (server *Server) authContext(writer http.ResponseWriter, request *http.Request) {
	if server.accounts == nil {
		writeError(writer, request, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "认证服务不可用", map[string]any{})
		return
	}
	contextView, err := server.accounts.Context(request.Context(), server.authCookieToken(request))
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if contextView.Session == nil && server.authCookieToken(request) != "" {
		server.clearAuthCookies(writer)
	}
	server.writeAuthContext(writer, http.StatusOK, contextView)
}

func (server *Server) authInitialize(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		SetupCode string `json:"setupCode"`
		newAccountCredentialRequest
	}
	if !decodeNewAccountCredential(writer, request, &body, "初始化请求无效") {
		return
	}
	session, err := server.accounts.InitializeRateLimited(request.Context(), accounts.InitializeRequest{
		SetupCode: body.SetupCode, Username: body.Username, DisplayName: body.DisplayName,
		Password: body.Password, PasswordConfirmation: body.PasswordConfirmation,
	}, server.authenticationClientIP(request))
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	server.writeAuthenticatedSession(writer, http.StatusCreated, session)
}

func (server *Server) writeAuthenticatedSession(writer http.ResponseWriter, status int, session accounts.Session) {
	server.setAuthCookie(writer, session)
	server.writeAuthContext(writer, status, accounts.Context{
		InstanceState: "READY", Mode: server.config.Mode, Session: &session,
	})
}

func (server *Server) authLogin(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "登录请求无效", map[string]any{})
		return
	}
	session, err := server.accounts.LoginRateLimited(
		request.Context(), body.Username, body.Password, server.authenticationClientIP(request),
	)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	server.setAuthCookie(writer, session)
	server.writeAuthContext(writer, http.StatusOK, accounts.Context{
		InstanceState: "READY", Mode: server.config.Mode, Session: &session,
		TestDefaultAccountActive: server.config.Mode == "test" && body.Username == "test" && body.Password == "test",
	})
}

func (server *Server) authLogout(writer http.ResponseWriter, request *http.Request) {
	token := server.authCookieToken(request)
	if token != "" && !server.revokeLogoutSession(writer, request, token) {
		return
	}
	server.clearAuthCookies(writer)
	writer.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) revokeLogoutSession(writer http.ResponseWriter, request *http.Request, token string) bool {
	session, err := server.accounts.Authenticate(request.Context(), token)
	if err != nil {
		return true
	}
	if !accounts.MatchesCSRF(token, request.Header.Get("X-Retrom-Csrf")) {
		writeError(writer, request, http.StatusForbidden, "CSRF_VALIDATION_FAILED", "请求验证失败", map[string]any{})
		return false
	}
	if err := server.accounts.Logout(request.Context(), session.Principal.SessionID); err != nil {
		server.databaseError(writer, request, err)
		return false
	}
	return true
}

func (server *Server) authChangePassword(writer http.ResponseWriter, request *http.Request) {
	principal, ok := authn.PrincipalFromContext(request.Context())
	if !ok {
		writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "需要登录", map[string]any{})
		return
	}
	var body struct {
		CurrentPassword         string `json:"currentPassword"`
		NewPassword             string `json:"newPassword"`
		NewPasswordConfirmation string `json:"newPasswordConfirmation"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "密码请求无效", map[string]any{})
		return
	}
	session, err := server.accounts.ChangePassword(
		request.Context(), principal, body.CurrentPassword, body.NewPassword, body.NewPasswordConfirmation,
	)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	server.setAuthCookie(writer, session)
	server.writeAuthContext(writer, http.StatusOK, accounts.Context{
		InstanceState: "READY", Mode: server.config.Mode, Session: &session,
	})
}

func (server *Server) writeAuthContext(writer http.ResponseWriter, status int, contextView accounts.Context) {
	authenticationState := "UNAUTHENTICATED"
	var user any
	var csrf any
	var idle any
	var absolute any
	if contextView.InstanceState == "INITIALIZATION_REQUIRED" {
		authenticationState = "NOT_APPLICABLE"
	}
	if contextView.Session != nil {
		authenticationState = "AUTHENTICATED"
		user = contextView.Session.User
		csrf = contextView.Session.CSRFToken
		idle = contextView.Session.IdleExpiresAtMS
		absolute = contextView.Session.AbsoluteExpiresAtMS
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Pragma", "no-cache")
	writeJSON(writer, status, map[string]any{
		"instanceState": contextView.InstanceState, "mode": contextView.Mode,
		"authenticationState": authenticationState, "user": user, "csrfToken": csrf,
		"idleExpiresAtMs": idle, "absoluteExpiresAtMs": absolute,
		"testDefaultAccountActive": contextView.TestDefaultAccountActive,
	})
}

func (server *Server) writeAccountError(writer http.ResponseWriter, request *http.Request, err error) {
	if server.writeAdminAccountError(writer, request, err) {
		return
	}
	var password *authn.PasswordError
	switch {
	case errors.Is(err, accounts.ErrRateLimited):
		writer.Header().Set("Retry-After", strconv.Itoa(accounts.RateLimitRetryAfter(err)))
		writeError(writer, request, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "认证请求过于频繁", map[string]any{})
	case errors.Is(err, accounts.ErrAuthentication):
		writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_FAILED", "用户名或密码不正确", map[string]any{})
	case errors.Is(err, accounts.ErrInitializationProof):
		writeError(writer, request, http.StatusUnauthorized, "INITIALIZATION_PROOF_INVALID", "初始化码无效", map[string]any{})
	case errors.Is(err, accounts.ErrInitializationDone):
		writeError(writer, request, http.StatusConflict, "INITIALIZATION_ALREADY_COMPLETED", "实例已完成初始化", map[string]any{})
	case errors.Is(err, accounts.ErrAccountLinkUnavailable):
		writeError(writer, request, http.StatusNotFound, "ACCOUNT_LINK_UNAVAILABLE", "账号链接不可用", map[string]any{})
	case errors.Is(err, accounts.ErrUsernameUnavailable):
		writeError(writer, request, http.StatusConflict, "USERNAME_UNAVAILABLE", "用户名不可用", map[string]any{})
	case errors.Is(err, accounts.ErrUserNotFound):
		writeError(writer, request, http.StatusNotFound, "USER_NOT_FOUND", "用户不存在", map[string]any{})
	case errors.Is(err, accounts.ErrIdempotencyReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
	case errors.As(err, &password):
		writeError(
			writer, request, http.StatusUnprocessableEntity, "PASSWORD_POLICY_VIOLATION", "密码不符合要求",
			map[string]any{"reasonCode": password.Reason},
		)
	case errors.Is(err, authn.ErrUsernameInvalid), errors.Is(err, authn.ErrDisplayInvalid):
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "账号信息无效", map[string]any{})
	default:
		server.databaseError(writer, request, err)
	}
}

func (server *Server) authCookieToken(request *http.Request) string {
	for _, name := range []string{server.authCookieName(), "retrom_session", "__Host-retrom_session"} {
		if cookie, err := request.Cookie(name); err == nil {
			return cookie.Value
		}
	}
	return ""
}

func (server *Server) authCookieName() string {
	if server.config.PublicOrigin != nil && server.config.PublicOrigin.Scheme == "https" {
		return "__Host-retrom_session"
	}
	return "retrom_session"
}

func (server *Server) setAuthCookie(writer http.ResponseWriter, session accounts.Session) {
	maximumAge := int((session.AbsoluteExpiresAtMS - server.now().UTC().UnixMilli()) / 1000)
	if maximumAge < 1 {
		maximumAge = int((24 * time.Hour).Seconds())
	}
	http.SetCookie(writer, &http.Cookie{
		Name: server.authCookieName(), Value: session.CookieToken, Path: "/", MaxAge: maximumAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: server.config.PublicOrigin != nil && server.config.PublicOrigin.Scheme == "https",
	})
}

func (server *Server) clearAuthCookies(writer http.ResponseWriter) {
	for _, name := range []string{"retrom_session", "__Host-retrom_session"} {
		http.SetCookie(writer, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   name == "__Host-retrom_session",
		})
	}
}
