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

const immersiveGameOperationID = "getImmersivePlatformGames"

func immersiveAssetURL(assetID *string) any {
	if assetID == nil {
		return nil
	}
	return "/content/assets/" + *assetID
}

func immersivePlatformProjection(platform immersive.Platform) map[string]any {
	return map[string]any{
		"platformId":     platform.ID,
		"platformName":   platform.Name,
		"gameCount":      platform.GameCount,
		"lastPlayedAtMs": platform.LastPlayedAtMS,
	}
}

func immersiveGameProjection(game immersive.Game) map[string]any {
	return map[string]any{
		"gameId":      game.ID,
		"title":       game.Title,
		"description": game.Description,
		"releaseYear": game.ReleaseYear,
		"developer":   game.Developer,
		"genre":       game.Genre,
		"platformInstance": map[string]any{
			"id": game.PlatformInstance.ID, "name": game.PlatformInstance.Name,
		},
		"defaultCore": map[string]any{
			"id": game.DefaultCore.ID, "name": game.DefaultCore.Name,
		},
		"coverUrl":       immersiveAssetURL(game.CoverAssetID),
		"videoUrl":       immersiveAssetURL(game.VideoAssetID),
		"lastPlayedAtMs": game.LastPlayedAtMS,
	}
}

func (server *Server) immersivePlatforms(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	platforms, err := server.immersive.Platforms(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(platforms))
	for _, platform := range platforms {
		items = append(items, immersivePlatformProjection(platform))
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(),
		"items":         items,
	})
}

func immersiveGameLimit(raw string) (int, error) {
	if raw == "" {
		return immersive.PageLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > immersive.PageLimit || strconv.Itoa(limit) != raw {
		return 0, errInvalidLimit
	}
	return limit, nil
}

func immersiveCursorDigest(profileID, platformID string, limit int) string {
	return cursor.FilterDigest(map[string]any{
		"profileId":  profileID,
		"platformId": platformID,
		"sort":       immersive.GameSortCode,
		"limit":      limit,
	})
}

func (server *Server) decodeImmersiveGameCursor(
	token, digest string,
) (immersive.GameCursor, error) {
	payload, err := server.cursors.Decode(
		token,
		immersiveGameOperationID,
		digest,
		immersive.GameSortCode,
	)
	if err != nil || len(payload.SortValues) != 1 {
		return immersive.GameCursor{}, errInvalidCursorPayload
	}
	return immersive.GameCursor{Title: payload.SortValues[0], ID: payload.ID}, nil
}

func (server *Server) encodeImmersiveGameCursor(
	digest string,
	next immersive.GameCursor,
) (string, error) {
	token, err := server.cursors.Encode(cursor.Payload{
		OperationID:  immersiveGameOperationID,
		FilterDigest: digest,
		SortCode:     immersive.GameSortCode,
		SortValues:   []string{next.Title},
		ID:           next.ID,
	})
	if err != nil {
		return "", fmt.Errorf("encode immersive game cursor: %w", err)
	}
	return token, nil
}

func (server *Server) immersivePlatformGames(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	platformID := request.PathValue("platformId")
	limit, err := immersiveGameLimit(request.URL.Query().Get("limit"))
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "分页大小无效", map[string]any{})
		return
	}
	digest := immersiveCursorDigest(principal.ProfileID, platformID, limit)
	var pageCursor *immersive.GameCursor
	if token := request.URL.Query().Get("cursor"); token != "" {
		decoded, decodeErr := server.decodeImmersiveGameCursor(token, digest)
		if decodeErr != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		pageCursor = &decoded
	}
	page, err := server.immersive.Games(request.Context(), principal.ProfileID, platformID, limit, pageCursor)
	if errors.Is(err, immersive.ErrPlatformNotFound) {
		writeError(writer, request, http.StatusNotFound, "RESOURCE_NOT_FOUND", "游戏平台不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if page.NextCursor != nil {
		token, encodeErr := server.encodeImmersiveGameCursor(digest, *page.NextCursor)
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		nextCursor = token
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, game := range page.Items {
		items = append(items, immersiveGameProjection(game))
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"generatedAtMs": server.now().UnixMilli(),
		"platform":      immersivePlatformProjection(page.Platform),
		"items":         items,
		"nextCursor":    nextCursor,
	})
}
