package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	homeservice "retrom/internal/service/home"
)

func recentQuery(values url.Values, profileID string) (homeservice.RecentQuery, error) {
	query := homeservice.RecentQuery{
		ProfileID: profileID, IncludeDeleted: true, Limit: 50,
		Query:      strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " ")),
		PlatformID: values.Get("platformId"), Sort: values.Get("sort"),
	}
	if query.Sort == "" {
		query.Sort = homeservice.RecentSortRecent
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return query, homeservice.ErrInvalidRecentQuery
		}
		query.Limit = limit
	}
	if raw := values.Get("fromAtMs"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return query, homeservice.ErrInvalidRecentQuery
		}
		query.FromAtMS = &value
	}
	if err := query.Validate(); err != nil {
		return query, fmt.Errorf("validate recent query: %w", err)
	}
	return query, nil
}

func recentDigest(query homeservice.RecentQuery) string {
	return cursor.FilterDigest(map[string]any{
		"profileId": query.ProfileID, "q": query.Query,
		"platformId": query.PlatformID, "sort": query.Sort, "fromAtMs": query.FromAtMS,
	})
}

func (server *Server) recentGames(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	query, err := recentQuery(request.URL.Query(), principal.ProfileID)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "最近游玩筛选无效", map[string]any{})
		return
	}
	digest := recentDigest(query)
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, decodeErr := server.cursors.Decode(token, "getRecentGames", digest, query.Sort)
		query.Cursor = &homeservice.RecentCursor{Values: payload.SortValues, GameID: payload.ID}
		if decodeErr != nil || query.Validate() != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
	}
	page, err := server.libraryDeps.Home.RecentPage(request.Context(), query)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]recentGameProjection, 0, len(page.Items))
	for _, game := range page.Items {
		items = append(items, projectHomeRecentGame(game))
	}
	var nextCursor any
	if page.NextCursor != nil {
		nextCursor, err = server.cursors.Encode(cursor.Payload{
			OperationID: "getRecentGames", FilterDigest: digest,
			SortCode: query.Sort, SortValues: page.NextCursor.Values, ID: page.NextCursor.GameID,
		})
		if err != nil {
			server.databaseError(writer, request, fmt.Errorf("encode recent cursor: %w", err))
			return
		}
	}
	response := map[string]any{"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor}
	if query.Cursor == nil {
		overview, err := server.libraryDeps.Home.RecentOverview(request.Context(), query)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		response["stats"], response["filteredCount"] = overview.Stats, overview.FilteredCount
		platforms := make([]homePlatform, 0, len(overview.Platforms))
		for _, platform := range overview.Platforms {
			platforms = append(platforms, homePlatform{
				ID: platform.ID, Name: platform.Name,
				GameCount: platform.GameCount, PlayCount: platform.PlayCount,
			})
		}
		response["platforms"] = platforms
	}
	writeJSON(writer, http.StatusOK, response)
}
