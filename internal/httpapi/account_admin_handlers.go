package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/cursor"
)

func (server *Server) accountLinkURL(path, fragment, token string) string {
	origin := strings.TrimRight(server.config.PublicOrigin.String(), "/")
	return origin + path + "#" + fragment + "=" + url.QueryEscape(token)
}

func (server *Server) authAccountLinkInspect(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		ExpectedKind string `json:"expectedKind"`
		Token        string `json:"token"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "链接请求无效", map[string]any{})
		return
	}
	result, err := server.accounts.InspectAccountLink(request.Context(), body.ExpectedKind, body.Token)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) authInvitationAccept(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Token string `json:"token"`
		newAccountCredentialRequest
	}
	if !decodeNewAccountCredential(writer, request, &body, "注册请求无效") {
		return
	}
	session, err := server.accounts.AcceptInvitation(request.Context(), accounts.AcceptInvitationRequest{
		Token: body.Token, Username: body.Username, DisplayName: body.DisplayName,
		Password: body.Password, PasswordConfirmation: body.PasswordConfirmation,
	})
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	server.writeAuthenticatedSession(writer, http.StatusCreated, session)
}

func (server *Server) authPasswordResetComplete(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Token                string `json:"token"`
		Password             string `json:"password"`
		PasswordConfirmation string `json:"passwordConfirmation"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "密码重置请求无效", map[string]any{})
		return
	}
	result, err := server.accounts.CompletePasswordReset(request.Context(), accounts.CompletePasswordResetRequest{
		Token: body.Token, Password: body.Password, PasswordConfirmation: body.PasswordConfirmation,
	})
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	if result.Session == nil {
		writer.Header().Set("Cache-Control", "private, no-store")
		writeJSON(writer, http.StatusOK, map[string]string{"status": result.Status})
		return
	}
	server.setAuthCookie(writer, *result.Session)
	server.writeAuthContext(writer, http.StatusOK, accounts.Context{
		InstanceState: "READY", Mode: server.config.Mode, Session: result.Session,
	})
}

func (server *Server) adminCreateInvitation(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		Role             string `json:"role"`
		ConfirmAdminRole bool   `json:"confirmAdminRole"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "邀请请求无效", map[string]any{})
		return
	}
	result, replayed, err := server.accounts.CreateInvitation(
		request.Context(), principal, body.Role, body.ConfirmAdminRole, key,
	)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	if replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusCreated, map[string]any{
		"accountLinkId": result.AccountLinkID, "role": result.Role, "state": result.State,
		"version": result.Version, "createdAtMs": result.CreatedAtMS, "expiresAtMs": result.ExpiresAtMS,
		"url": server.accountLinkURL("/register", "invite", result.CapabilityToken),
	})
}

func normalizedAdminUserQuery(value string) string {
	return norm.NFC.String(strings.TrimFunc(value, unicode.IsSpace))
}

func (server *Server) adminUsers(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	values := request.URL.Query()
	queryText := normalizedAdminUserQuery(values.Get("q"))
	if values.Has("q") && queryText == "" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "用户搜索词不能为空", map[string]any{})
		return
	}
	status, sortCode := values.Get("status"), values.Get("sort")
	if status == "" {
		status = "NON_DELETED"
	}
	if sortCode == "" {
		sortCode = "CREATED_DESC"
	}
	filterDigest := cursor.FilterDigest(map[string]any{
		"principalId": principal.UserID, "q": queryText, "role": values.Get("role"),
		"sort": sortCode, "status": status,
	})
	filter := accounts.UserListFilter{
		Query: queryText, Role: values.Get("role"), Status: status, Sort: sortCode, Limit: 51,
	}
	limit := 50
	if values.Get("limit") != "" {
		limit, _ = strconv.Atoi(values.Get("limit"))
		filter.Limit = limit + 1
	}
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminUsers", filterDigest, sortCode)
		if err != nil {
			writeError(writer, request, http.StatusBadRequest, "CURSOR_INVALID", "用户分页游标无效", map[string]any{})
			return
		}
		filter.AfterValues, filter.AfterID = payload.SortValues, payload.ID
	}
	items, err := server.accounts.ListUsers(request.Context(), filter)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "用户筛选无效", map[string]any{})
		return
	}
	var nextCursor any
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		token, encodeErr := server.cursors.Encode(cursor.Payload{
			OperationID: "getAdminUsers", FilterDigest: filterDigest, SortCode: sortCode,
			SortValues: adminUserCursorSortValues(last, sortCode), ID: last.UserID,
		})
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		nextCursor = token
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor,
	})
}

func adminUserCursorSortValues(user accounts.AdminUser, sortCode string) []string {
	switch sortCode {
	case "USERNAME_ASC":
		return []string{user.Username}
	case "LAST_LOGIN_DESC":
		lastLogin := int64(-1)
		if value, ok := user.LastLoginAtMS.(int64); ok {
			lastLogin = value
		}
		return []string{strconv.FormatInt(lastLogin, 10), strconv.FormatInt(user.CreatedAtMS, 10)}
	default:
		return []string{strconv.FormatInt(user.CreatedAtMS, 10)}
	}
}

func (server *Server) adminUser(writer http.ResponseWriter, request *http.Request) {
	result, err := server.accounts.GetUser(request.Context(), request.PathValue("userId"))
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) adminPatchUser(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前资源版本", map[string]any{})
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		Role             *string `json:"role"`
		Status           *string `json:"status"`
		ConfirmAdminRole bool    `json:"confirmAdminRole"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "用户修改请求无效", map[string]any{})
		return
	}
	result, replayed, err := server.accounts.UpdateUser(
		request.Context(), principal, request.PathValue("userId"), expected,
		accounts.UserPatch{Role: body.Role, Status: body.Status, ConfirmAdminRole: body.ConfirmAdminRole}, key,
	)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	if replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) adminDeleteUser(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前资源版本", map[string]any{})
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		ConfirmUsername string `json:"confirmUsername"`
	}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "用户删除请求无效", map[string]any{})
		return
	}
	replayed, err := server.accounts.DeleteUser(
		request.Context(), principal, request.PathValue("userId"), expected, body.ConfirmUsername, key,
	)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	if replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) adminInvitations(writer http.ResponseWriter, request *http.Request) {
	server.adminAccountLinks(writer, request, "INVITATION", "")
}

func (server *Server) adminPasswordResetLinks(writer http.ResponseWriter, request *http.Request) {
	server.adminAccountLinks(writer, request, "PASSWORD_RESET", request.PathValue("userId"))
}

func (server *Server) adminAccountLinks(
	writer http.ResponseWriter,
	request *http.Request,
	kind, targetUserID string,
) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	state := request.URL.Query().Get("state")
	if state == "" {
		state = "ACTIVE"
	}
	operationID := "getAdminInvitations"
	if kind == "PASSWORD_RESET" {
		operationID = "getAdminUserPasswordResetLinks"
		if _, err := server.accounts.GetUser(request.Context(), targetUserID); err != nil {
			server.writeAccountError(writer, request, err)
			return
		}
	}
	digest := cursor.FilterDigest(map[string]any{
		"kind": kind, "principalId": principal.UserID, "state": state, "targetUserId": targetUserID,
	})
	limit := 50
	if request.URL.Query().Get("limit") != "" {
		limit, _ = strconv.Atoi(request.URL.Query().Get("limit"))
	}
	filter := accounts.LinkListFilter{
		Kind: kind, TargetUserID: targetUserID, State: state, Limit: limit + 1,
	}
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, operationID, digest, "CREATED_DESC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "CURSOR_INVALID", "链接分页游标无效", map[string]any{})
			return
		}
		createdAt, parseErr := strconv.ParseInt(payload.SortValues[0], 10, 64)
		if parseErr != nil {
			writeError(writer, request, http.StatusBadRequest, "CURSOR_INVALID", "链接分页游标无效", map[string]any{})
			return
		}
		filter.AfterAtMS, filter.AfterID = createdAt, payload.ID
	}
	items, err := server.accounts.ListAccountLinks(request.Context(), filter)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "链接筛选无效", map[string]any{})
		return
	}
	var nextCursor any
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		token, encodeErr := server.cursors.Encode(cursor.Payload{
			OperationID: operationID, FilterDigest: digest, SortCode: "CREATED_DESC",
			SortValues: []string{strconv.FormatInt(last.CreatedAtMS, 10)}, ID: last.AccountLinkID,
		})
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		nextCursor = token
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor,
	})
}

func (server *Server) adminCreatePasswordReset(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前资源版本", map[string]any{})
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "密码重置链接请求无效", map[string]any{})
		return
	}
	result, replayed, err := server.accounts.CreatePasswordReset(
		request.Context(), principal, request.PathValue("userId"), expected, key,
	)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	if replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.TargetVersion))
	writer.Header().Set("Cache-Control", "private, no-store")
	writeJSON(writer, http.StatusCreated, map[string]any{
		"accountLinkId": result.AccountLinkID, "targetUserId": result.TargetUserID,
		"targetUserVersion": result.TargetVersion, "state": result.State, "version": result.Version,
		"createdAtMs": result.CreatedAtMS, "expiresAtMs": result.ExpiresAtMS,
		"url": server.accountLinkURL("/reset-password", "reset", result.CapabilityToken),
	})
}

func (server *Server) adminRevokeAccountLink(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前资源版本", map[string]any{})
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 32<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "链接撤销请求无效", map[string]any{})
		return
	}
	replayed, err := server.accounts.RevokeAccountLink(
		request.Context(), principal, request.PathValue("accountLinkId"), expected, key,
	)
	if err != nil {
		server.writeAccountError(writer, request, err)
		return
	}
	if replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, expected+1))
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func accountVersionError(writer http.ResponseWriter, request *http.Request) {
	writeError(
		writer, request, http.StatusPreconditionFailed, "RESOURCE_VERSION_CONFLICT", "资源版本已变化", map[string]any{},
	)
}

func (server *Server) writeAdminAccountError(writer http.ResponseWriter, request *http.Request, err error) bool {
	switch {
	case errors.Is(err, accounts.ErrUserVersion):
		accountVersionError(writer, request)
	case errors.Is(err, accounts.ErrUserSelfChange):
		writeError(writer, request, http.StatusConflict, "USER_SELF_MANAGEMENT_FORBIDDEN", "不能对当前账号执行此操作", map[string]any{})
	case errors.Is(err, accounts.ErrLastAdmin):
		writeError(writer, request, http.StatusConflict, "USER_LAST_ADMIN_REQUIRED", "必须保留至少一名可用管理员", map[string]any{})
	case errors.Is(err, accounts.ErrUserNoChange):
		writeError(writer, request, http.StatusConflict, "USER_NO_STATE_CHANGE", "账号状态没有变化", map[string]any{})
	case errors.Is(err, accounts.ErrUserDeleted):
		writeError(writer, request, http.StatusConflict, "USER_ALREADY_DELETED", "账号已删除", map[string]any{})
	case errors.Is(err, accounts.ErrUserTransition):
		writeError(writer, request, http.StatusConflict, "USER_INVALID_TRANSITION", "账号状态转换无效", map[string]any{})
	case errors.Is(err, accounts.ErrConfirmation):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"USER_DELETE_CONFIRMATION_MISMATCH", "删除确认名不匹配", map[string]any{},
		)
	case errors.Is(err, accounts.ErrRoleConfirmation):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"ADMIN_ROLE_CONFIRMATION_REQUIRED", "需要确认管理员权限", map[string]any{},
		)
	case errors.Is(err, accounts.ErrAccountLinkNotActive):
		writeError(writer, request, http.StatusConflict, "ACCOUNT_LINK_NOT_ACTIVE", "链接不再可撤销", map[string]any{})
	default:
		return false
	}
	return true
}
