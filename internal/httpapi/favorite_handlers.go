package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	"retrom/internal/favorites"
)

func favoritePrincipal(request *http.Request) favorites.Principal {
	principal, _ := authn.PrincipalFromContext(request.Context())
	return favorites.Principal{UserID: principal.UserID, ProfileID: principal.ProfileID}
}

func writeFavoriteError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, favorites.ErrGameNotFound):
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
	case errors.Is(err, favorites.ErrFolderNotFound):
		writeError(writer, request, http.StatusNotFound, "FAVORITE_FOLDER_NOT_FOUND", "收藏夹不存在", map[string]any{})
	case errors.Is(err, favorites.ErrFolderNameConflict):
		writeError(writer, request, http.StatusConflict, "FAVORITE_FOLDER_NAME_CONFLICT", "已存在同名收藏夹", map[string]any{})
	case errors.Is(err, favorites.ErrIdempotencyReused):
		writeError(writer, request, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "幂等键已用于另一请求", map[string]any{})
	case errors.Is(err, favorites.ErrVersionConflict):
		writeError(writer, request, http.StatusPreconditionFailed, "RESOURCE_VERSION_CONFLICT", "收藏夹已被修改", map[string]any{})
	case errors.Is(err, favorites.ErrBatchTooLarge):
		writeError(
			writer, request, http.StatusRequestEntityTooLarge,
			"FAVORITE_BATCH_TOO_LARGE", "收藏批量请求超过限制", map[string]any{},
		)
	case errors.Is(err, favorites.ErrFolderLimit):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"FAVORITE_FOLDER_LIMIT_REACHED", "收藏夹数量已达上限", map[string]any{},
		)
	case errors.Is(err, favorites.ErrInvalidCursor):
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
	case errors.Is(err, favorites.ErrInvalid), errors.Is(err, favorites.ErrInvalidFolderName),
		errors.Is(err, favorites.ErrInvalidFavoriteListSort):
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "收藏请求无效", map[string]any{})
	default:
		serverError(writer, request, err)
	}
}

func serverError(writer http.ResponseWriter, request *http.Request, err error) {
	slog.ErrorContext(
		request.Context(), "favorite operation failed", "requestId",
		request.Context().Value(requestIDKey), "error", err,
	)
	writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "数据库操作失败", map[string]any{})
}

func writeIdempotentFavoriteResponse(writer http.ResponseWriter, response favorites.IdempotentResponse) {
	for name, value := range response.Headers {
		writer.Header().Set(name, value)
	}
	writer.Header().Set("Cache-Control", "private, no-store")
	if response.Replayed {
		writer.Header().Set("X-Retrom-Idempotent-Replay", "true")
	}
	writer.WriteHeader(response.Status)
	if len(response.Body) > 0 {
		_, _ = writer.Write(response.Body)
	}
}

func favoriteListFilterDigest(request *http.Request, scope, sortCode string) string {
	principal, _ := authn.PrincipalFromContext(request.Context())
	values := request.URL.Query()
	return cursor.FilterDigest(map[string]any{
		"principalId": principal.UserID,
		"scope":       scope,
		"folderId":    values.Get("folderId"),
		"q":           strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " ")),
		"platformId":  values.Get("platformId"),
		"sort":        sortCode,
	})
}

func parseFavoriteListOptions(request *http.Request) (favorites.ListOptions, string, error) {
	values := request.URL.Query()
	scope := values.Get("scope")
	if scope == "" {
		scope = favorites.ScopeAll
	}
	sortCode := values.Get("sort")
	if sortCode == "" {
		sortCode = favorites.SortFavoritedDesc
	}
	folderID := values.Get("folderId")
	if (scope == favorites.ScopeFolder) != (folderID != "") ||
		(folderID != "" && !favorites.ValidID(folderID)) {
		return favorites.ListOptions{}, "", errUnknownQuery
	}
	if scope != favorites.ScopeAll && scope != favorites.ScopeUncategorized && scope != favorites.ScopeFolder {
		return favorites.ListOptions{}, "", errUnknownQuery
	}
	if sortCode != favorites.SortFavoritedDesc && sortCode != favorites.SortRecentlyPlayed &&
		sortCode != favorites.SortTitleAsc && sortCode != favorites.SortReleaseYearDesc {
		return favorites.ListOptions{}, "", errUnknownQuery
	}
	limit := 50
	if values.Get("limit") != "" {
		limit, _ = strconv.Atoi(values.Get("limit"))
	}
	options := favorites.ListOptions{
		Scope: scope, FolderID: folderID, Query: values.Get("q"), PlatformID: values.Get("platformId"),
		Sort: sortCode, Limit: limit,
	}
	filterDigest := favoriteListFilterDigest(request, scope, sortCode)
	return options, filterDigest, nil
}

func (server *Server) favoritesList(writer http.ResponseWriter, request *http.Request) {
	options, filterDigest, err := parseFavoriteListOptions(request)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "收藏筛选无效", map[string]any{})
		return
	}
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, decodeErr := server.cursors.Decode(token, "getFavorites", filterDigest, options.Sort)
		if decodeErr != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		options.Cursor = &favorites.PageCursor{SortValues: payload.SortValues, ID: payload.ID}
	}
	result, err := server.favoriteService.List(request.Context(), favoritePrincipal(request), options)
	if errors.Is(err, favorites.ErrFolderNotFound) {
		writeFavoriteError(writer, request, err)
		return
	}
	if errors.Is(err, favorites.ErrInvalidCursor) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if result.NextCursor != nil {
		token, encodeErr := server.cursors.Encode(cursor.Payload{
			OperationID: "getFavorites", FilterDigest: filterDigest, SortCode: options.Sort,
			SortValues: result.NextCursor.SortValues, ID: result.NextCursor.ID,
		})
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		nextCursor = token
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": result.GeneratedAtMS, "summary": result.Summary, "folders": result.Folders,
		"platforms": result.Platforms, "totalCount": result.TotalCount, "items": result.Items,
		"nextCursor": nextCursor,
	})
}

func (server *Server) putFavorite(writer http.ResponseWriter, request *http.Request) {
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		return
	}
	state, err := server.favoriteService.Favorite(
		request.Context(), favoritePrincipal(request), request.PathValue("gameId"),
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (server *Server) putFavoriteFolders(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		FolderIDs []string `json:"folderIds"`
	}
	if err := decodeJSON(writer, request, &body, 16384); err != nil {
		return
	}
	if body.FolderIDs == nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "folderIds 必填", map[string]any{})
		return
	}
	state, err := server.favoriteService.ReplaceFolders(
		request.Context(), favoritePrincipal(request), request.PathValue("gameId"), body.FolderIDs,
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func requireFavoriteIdempotency(writer http.ResponseWriter, request *http.Request) (string, bool) {
	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return "", false
	}
	return key, true
}

func (server *Server) organizeFavorites(writer http.ResponseWriter, request *http.Request) {
	key, ok := requireFavoriteIdempotency(writer, request)
	if !ok {
		return
	}
	var body struct {
		GameIDs         []string `json:"gameIds"`
		AddFolderIDs    []string `json:"addFolderIds"`
		RemoveFolderIDs []string `json:"removeFolderIds"`
	}
	if err := decodeJSON(writer, request, &body, 65536); err != nil {
		return
	}
	response, err := server.favoriteService.Organize(
		request.Context(), favoritePrincipal(request), key,
		body.GameIDs, body.AddFolderIDs, body.RemoveFolderIDs,
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeIdempotentFavoriteResponse(writer, response)
}

func (server *Server) unfavorite(writer http.ResponseWriter, request *http.Request) {
	key, ok := requireFavoriteIdempotency(writer, request)
	if !ok {
		return
	}
	var body struct {
		GameIDs []string `json:"gameIds"`
	}
	if err := decodeJSON(writer, request, &body, 65536); err != nil {
		return
	}
	response, err := server.favoriteService.Unfavorite(
		request.Context(), favoritePrincipal(request), key, body.GameIDs,
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeIdempotentFavoriteResponse(writer, response)
}

func (server *Server) restoreFavorites(writer http.ResponseWriter, request *http.Request) {
	key, ok := requireFavoriteIdempotency(writer, request)
	if !ok {
		return
	}
	var body struct {
		Items []favorites.RestoreItem `json:"items"`
	}
	if err := decodeJSON(writer, request, &body, 262144); err != nil {
		return
	}
	response, err := server.favoriteService.Restore(
		request.Context(), favoritePrincipal(request), key, body.Items,
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeIdempotentFavoriteResponse(writer, response)
}

func (server *Server) createFavoriteFolder(writer http.ResponseWriter, request *http.Request) {
	key, ok := requireFavoriteIdempotency(writer, request)
	if !ok {
		return
	}
	var body struct {
		Name           string   `json:"name"`
		InitialGameIDs []string `json:"initialGameIds"`
	}
	if err := decodeJSON(writer, request, &body, 65536); err != nil {
		return
	}
	if body.InitialGameIDs == nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "initialGameIds 必填", map[string]any{})
		return
	}
	response, err := server.favoriteService.CreateFolder(
		request.Context(), favoritePrincipal(request), key, body.Name, body.InitialGameIDs,
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeIdempotentFavoriteResponse(writer, response)
}

func requireFavoriteVersion(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	expected, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前收藏夹版本", map[string]any{})
		return 0, false
	}
	return expected, true
}

func (server *Server) patchFavoriteFolder(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireFavoriteVersion(writer, request)
	if !ok {
		return
	}
	key, ok := requireFavoriteIdempotency(writer, request)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		return
	}
	response, err := server.favoriteService.RenameFolder(
		request.Context(), favoritePrincipal(request), key, request.PathValue("folderId"), body.Name, expected,
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeIdempotentFavoriteResponse(writer, response)
}

func (server *Server) deleteFavoriteFolder(writer http.ResponseWriter, request *http.Request) {
	expected, ok := requireFavoriteVersion(writer, request)
	if !ok {
		return
	}
	key, ok := requireFavoriteIdempotency(writer, request)
	if !ok {
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		return
	}
	response, err := server.favoriteService.DeleteFolder(
		request.Context(), favoritePrincipal(request), key, request.PathValue("folderId"), expected,
	)
	if err != nil {
		writeFavoriteError(writer, request, err)
		return
	}
	writeIdempotentFavoriteResponse(writer, response)
}
