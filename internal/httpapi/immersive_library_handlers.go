package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	"retrom/internal/immersive"
)

const immersiveLibraryGameOperationID = "getImmersiveLibraryGames"

func immersiveDestinationProjection(destination immersive.Destination) map[string]any {
	featuredGames := make([]map[string]any, 0, len(destination.FeaturedGames))
	for _, game := range destination.FeaturedGames {
		featuredGames = append(featuredGames, map[string]any{
			"gameId":         game.ID,
			"title":          game.Title,
			"coverUrl":       immersiveAssetURL(game.CoverAssetID),
			"lastPlayedAtMs": game.LastPlayedAtMS,
		})
	}
	return map[string]any{
		"destinationId":  destination.ID,
		"kind":           destination.Kind,
		"name":           destination.Name,
		"gameCount":      destination.GameCount,
		"lastPlayedAtMs": destination.LastPlayedAtMS,
		"featuredGames":  featuredGames,
	}
}

func (server *Server) immersiveDestinations(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	destinations, err := server.immersive.Destinations(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(destinations))
	for _, destination := range destinations {
		items = append(items, immersiveDestinationProjection(destination))
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(),
		"items":         items,
	})
}

func immersiveLibrarySortCode(kind string) string {
	if kind == immersive.LibraryRecent {
		return immersive.RecentGameSortCode
	}
	return immersive.GameSortCode
}

func immersiveLibraryCursorDigest(profileID, kind, folderID string, limit int) string {
	return cursor.FilterDigest(map[string]any{
		"profileId": profileID,
		"kind":      kind,
		"folderId":  folderID,
		"sort":      immersiveLibrarySortCode(kind),
		"limit":     limit,
	})
}

func (server *Server) decodeImmersiveLibraryCursor(
	token, digest, kind string,
) (immersive.GameCursor, error) {
	payload, err := server.cursors.Decode(
		token,
		immersiveLibraryGameOperationID,
		digest,
		immersiveLibrarySortCode(kind),
	)
	if err != nil {
		return immersive.GameCursor{}, errInvalidCursorPayload
	}
	if kind == immersive.LibraryRecent {
		if len(payload.SortValues) != 1 {
			return immersive.GameCursor{}, errInvalidCursorPayload
		}
		lastPlayedAtMS, parseErr := strconv.ParseInt(payload.SortValues[0], 10, 64)
		if parseErr != nil || lastPlayedAtMS < 0 {
			return immersive.GameCursor{}, errInvalidCursorPayload
		}
		return immersive.GameCursor{
			ID: payload.ID, LastPlayedAtMS: &lastPlayedAtMS,
		}, nil
	}
	if len(payload.SortValues) != 2 {
		return immersive.GameCursor{}, errInvalidCursorPayload
	}
	return immersive.GameCursor{
		TitleInitial: payload.SortValues[0],
		Title:        payload.SortValues[1],
		ID:           payload.ID,
	}, nil
}

func (server *Server) encodeImmersiveLibraryCursor(
	digest, kind string,
	next immersive.GameCursor,
) (string, error) {
	sortValues := []string{next.TitleInitial, next.Title}
	if kind == immersive.LibraryRecent {
		if next.LastPlayedAtMS == nil {
			return "", errInvalidCursorPayload
		}
		sortValues = []string{strconv.FormatInt(*next.LastPlayedAtMS, 10)}
	}
	token, err := server.cursors.Encode(cursor.Payload{
		OperationID:  immersiveLibraryGameOperationID,
		FilterDigest: digest,
		SortCode:     immersiveLibrarySortCode(kind),
		SortValues:   sortValues,
		ID:           next.ID,
	})
	if err != nil {
		return "", fmt.Errorf("encode immersive library cursor: %w", err)
	}
	return token, nil
}

func immersiveFavoriteFolderProjection(folder immersive.FavoriteFolder) map[string]any {
	return map[string]any{
		"folderId":  folder.ID,
		"name":      folder.Name,
		"gameCount": folder.GameCount,
	}
}

func (server *Server) immersiveLibraryGames(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	kind := request.PathValue("libraryKind")
	if !immersive.ValidLibraryKind(kind) {
		writeError(writer, request, http.StatusNotFound, "RESOURCE_NOT_FOUND", "沉浸游戏分类不存在", map[string]any{})
		return
	}
	limit, err := immersiveGameLimit(request.URL.Query().Get("limit"))
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "分页大小无效", map[string]any{})
		return
	}
	folderID := request.URL.Query().Get("folderId")
	digest := immersiveLibraryCursorDigest(principal.ProfileID, kind, folderID, limit)
	var pageCursor *immersive.GameCursor
	if token := request.URL.Query().Get("cursor"); token != "" {
		decoded, decodeErr := server.decodeImmersiveLibraryCursor(token, digest, kind)
		if decodeErr != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		pageCursor = &decoded
	}
	page, err := server.immersive.LibraryGames(
		request.Context(),
		principal.ProfileID,
		kind,
		folderID,
		limit,
		pageCursor,
	)
	if errors.Is(err, immersive.ErrLibraryNotFound) || errors.Is(err, immersive.ErrFavoriteFolderNotFound) {
		writeError(writer, request, http.StatusNotFound, "RESOURCE_NOT_FOUND", "沉浸游戏分类不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if page.NextCursor != nil {
		token, encodeErr := server.encodeImmersiveLibraryCursor(digest, kind, *page.NextCursor)
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		nextCursor = token
	}
	folders := make([]map[string]any, 0, len(page.Folders))
	for _, folder := range page.Folders {
		folders = append(folders, immersiveFavoriteFolderProjection(folder))
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, game := range page.Items {
		items = append(items, immersiveGameProjection(game))
	}
	var folder any
	if page.Folder != nil {
		folder = immersiveFavoriteFolderProjection(*page.Folder)
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(),
		"library":       immersiveDestinationProjection(page.Library),
		"folder":        folder,
		"folders":       folders,
		"items":         items,
		"nextCursor":    nextCursor,
	})
}
