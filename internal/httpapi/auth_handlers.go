package httpapi

import (
	"errors"
	"net/http"
	"time"

	"retrom/internal/accounts"
	"retrom/internal/authn"
)

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
		SetupCode            string `json:"setupCode"`
		Username             string `json:"username"`
		DisplayName          string `json:"displayName"`
		Password             string `json:"password"`
		PasswordConfirmation string `json:"passwordConfirmation"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "初始化请求无效", map[string]any{})
		return
	}
	session, err := server.accounts.Initialize(request.Context(), accounts.InitializeRequest{
		SetupCode: body.SetupCode, Username: body.Username, DisplayName: body.DisplayName,
		Password: body.Password, PasswordConfirmation: body.PasswordConfirmation,
	})
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	server.setAuthCookie(writer, session)
	server.writeAuthContext(writer, http.StatusCreated, accounts.Context{
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
	session, err := server.accounts.Login(request.Context(), body.Username, body.Password)
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
	if token != "" {
		session, err := server.accounts.Authenticate(request.Context(), token)
		if err == nil {
			if !accounts.MatchesCSRF(token, request.Header.Get("X-Retrom-CSRF")) {
				writeError(writer, request, http.StatusForbidden, "CSRF_VALIDATION_FAILED", "请求验证失败", map[string]any{})
				return
			}
			if err := server.accounts.Logout(request.Context(), session.Principal.SessionID); err != nil {
				server.databaseError(writer, request, err)
				return
			}
		}
	}
	server.clearAuthCookies(writer)
	writer.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
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
	var password *authn.PasswordError
	switch {
	case errors.Is(err, accounts.ErrAuthentication):
		writeError(writer, request, http.StatusUnauthorized, "AUTHENTICATION_FAILED", "用户名或密码不正确", map[string]any{})
	case errors.Is(err, accounts.ErrInitializationProof):
		writeError(writer, request, http.StatusUnauthorized, "INITIALIZATION_PROOF_INVALID", "初始化码无效", map[string]any{})
	case errors.Is(err, accounts.ErrInitializationDone):
		writeError(writer, request, http.StatusConflict, "INITIALIZATION_ALREADY_COMPLETED", "实例已完成初始化", map[string]any{})
	case errors.As(err, &password):
		writeError(writer, request, http.StatusUnprocessableEntity, "PASSWORD_POLICY_VIOLATION", "密码不符合要求", map[string]any{"reasonCode": password.Reason})
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
