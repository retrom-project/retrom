package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	gamelistservice "retrom/internal/service/gamelist"
	"retrom/internal/service/tagging"
)

func (server *Server) games(writer http.ResponseWriter, request *http.Request) {
	server.gameList(writer, request, false)
}

// Method dispatch and nullable detail projections stay at the route protocol boundary.
func (server *Server) game(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	gameID := request.PathValue("gameId")
	detail, err := server.gameListService.Detail(
		request.Context(), principal.ProfileID, gameID,
	)
	if errors.Is(err, gamelistservice.ErrNotFound) {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	favorite, tags, err := server.gameAssociations(request.Context(), principal.ProfileID, gameID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"gameId": gameID, "title": detail.Title, "description": detail.Description,
		"developer": detail.Developer, "publisher": detail.Publisher,
		"genre": detail.Genre, "players": gameListInt(detail.Players),
		"releaseYear": gameListInt(detail.ReleaseYear),
		"platform": map[string]any{
			"id": detail.Platform.ID, "name": detail.Platform.Name,
		},
		"platformInstance": map[string]any{
			"id": detail.PlatformInstance.ID, "name": detail.PlatformInstance.Name,
		},
		"version": detail.Version, "updatedAtMs": detail.UpdatedAtMS,
		"coverUrl":         gameListAssetURL(detail.CoverAssetID),
		"videoUrl":         gameListAssetURL(detail.VideoAssetID),
		"activeDurationMs": detail.ActiveDurationMS,
		"coreOptions":      projectGameCoreOptions(detail.CoreOptions),
		"dosEntries":       projectGameDOSEntries(detail.DOSEntries),
		"defaultDosEntry":  gameListString(detail.DefaultDOSEntry),
		"saveStateCount":   detail.SaveStateCount,
		"saveStates":       projectGameSaveStates(detail.SaveStates),
		"favorite":         favorite, "tags": tags,
	})
}

func gameListInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func gameListString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func gameListAssetURL(value *string) any {
	if value == nil {
		return nil
	}
	return "/content/assets/" + *value
}

func gameListDiscLabel(value *int64) any {
	if value == nil {
		return nil
	}
	return fmt.Sprintf("光盘 %d", *value+1)
}

func projectGameCoreOptions(options []gamelistservice.CoreOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		reasons := make([]map[string]any, 0, len(option.Reasons))
		for _, reason := range option.Reasons {
			reasons = append(reasons, map[string]any{"code": reason.Code, "level": reason.Level})
		}
		result = append(result, map[string]any{
			"coreId": option.CoreID, "name": option.Name, "isDefault": option.IsDefault,
			"status": option.Status, "revalidationStatus": option.RevalidationStatus,
			"variantId":  gameListString(option.VariantID),
			"providerId": gameListString(option.ProviderID), "targetId": gameListString(option.TargetID),
			"datVersionId":      gameListString(option.DATVersionID),
			"revalidationJobId": gameListString(option.RevalidationJobID),
			"requiresThreads":   option.RequiresThreads, "reasons": reasons,
		})
	}
	return result
}

func projectGameDOSEntries(entries []gamelistservice.DOSEntry) []map[string]any {
	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		result = append(result, map[string]any{
			"path": entry.Path, "originalPath": entry.OriginalPath, "kind": entry.Kind,
			"rank": entry.Rank, "enabled": entry.Enabled, "directLaunchSafe": entry.DirectLaunchSafe,
		})
	}
	return result
}

func projectGameSaveStates(states []gamelistservice.SaveState) []map[string]any {
	result := make([]map[string]any, 0, len(states))
	for _, state := range states {
		result = append(result, map[string]any{
			"saveStateId": state.ID, "name": state.Name, "createdAtMs": state.CreatedAtMS,
			"lastSyncedAtMs": gameListInt(state.LastSyncedAtMS),
			"discIndex":      gameListInt(state.DiscIndex), "discLabel": gameListDiscLabel(state.DiscIndex),
			"screenshotUrl": optionalSaveScreenshotURL(state.ID, state.HasScreenshot),
			"core":          map[string]any{"id": state.CoreID, "name": state.CoreName},
		})
	}
	return result
}

// gameCoreOptions remains as a small compatibility seam for existing package
// tests while the query itself is owned by the game list repository.
func (server *Server) gameCoreOptions(
	ctx context.Context, gameID string,
) ([]map[string]any, error) {
	detail, err := server.gameListService.Detail(ctx, "compatibility", gameID)
	if err != nil {
		return nil, fmt.Errorf("read game core options: %w", err)
	}
	return projectGameCoreOptions(detail.CoreOptions), nil
}

func (server *Server) adminGames(writer http.ResponseWriter, request *http.Request) {
	server.gameList(writer, request, true)
}

type gameListFilters struct {
	Filters     gamelistservice.Filters
	NormalizedQ string
}

func parseGameListFilters(values url.Values, includeDeleted bool) (gameListFilters, error) {
	filters := gameListFilters{}
	status := values.Get("status")
	switch {
	case !includeDeleted || status == "PUBLISHED":
		filters.Filters.Status = "PUBLISHED"
	case status == "DELETED":
		filters.Filters.Status = "DELETED"
	case status != "" && status != "ALL":
		return gameListFilters{}, fmt.Errorf("%w: game status", errUnknownQuery)
	}
	filters.NormalizedQ = strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " "))
	filters.Filters.Query = filters.NormalizedQ
	if tagID := values.Get("tagId"); tagID != "" {
		if !tagging.ValidID(tagID) {
			return gameListFilters{}, errInvalidGameTagFilter
		}
		filters.Filters.TagID = tagID
	}
	filters.Filters.PlatformID = values.Get("platformId")
	filters.Filters.PlatformInstanceID = values.Get("platformInstanceId")
	return filters, nil
}

func writeGameListFilterError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, errInvalidGameTagFilter) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "标签筛选无效", map[string]any{})
		return
	}
	writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏状态筛选无效", map[string]any{})
}

func gameListSortCode(raw string, includeDeleted bool) (string, error) {
	if raw == "" {
		if includeDeleted {
			return gamelistservice.SortUpdatedDesc, nil
		}
		return gamelistservice.SortRecentDesc, nil
	}
	switch raw {
	case gamelistservice.SortTitleAsc, gamelistservice.SortAddedDesc:
		return raw, nil
	case gamelistservice.SortRecentDesc:
		if !includeDeleted {
			return raw, nil
		}
	case gamelistservice.SortUpdatedDesc:
		if includeDeleted {
			return raw, nil
		}
	}
	return "", errUnknownQuery
}

func (server *Server) applyGameListCursor(
	token string,
	operationID string,
	filterDigest string,
	sortCode string,
) (*gamelistservice.Cursor, error) {
	if token == "" {
		//nolint:nilnil // an absent cursor is a valid unbounded first page
		return nil, nil
	}
	payload, err := server.cursors.Decode(token, operationID, filterDigest, sortCode)
	if err != nil {
		return nil, errInvalidCursorPayload
	}
	switch sortCode {
	case gamelistservice.SortTitleAsc:
		if len(payload.SortValues) != 1 {
			return nil, errInvalidCursorPayload
		}
	case gamelistservice.SortAddedDesc, gamelistservice.SortUpdatedDesc:
		if len(payload.SortValues) != 2 {
			return nil, errInvalidCursorPayload
		}
		if _, err := strconv.ParseInt(payload.SortValues[0], 10, 64); err != nil {
			return nil, errInvalidCursorPayload
		}
	case gamelistservice.SortRecentDesc:
		if len(payload.SortValues) != 3 {
			return nil, errInvalidCursorPayload
		}
		if _, err := strconv.ParseInt(payload.SortValues[0], 10, 64); err != nil {
			return nil, errInvalidCursorPayload
		}
		if _, err := strconv.ParseInt(payload.SortValues[1], 10, 64); err != nil {
			return nil, errInvalidCursorPayload
		}
	default:
		return nil, errInvalidCursorPayload
	}
	return &gamelistservice.Cursor{SortValues: payload.SortValues, ID: payload.ID}, nil
}

func projectGameListItem(item gamelistservice.GameItem, includeAdminProjection bool) map[string]any {
	result := map[string]any{
		"gameId": item.ID, "title": item.Title,
		"platform": map[string]any{"id": item.Platform.ID, "name": item.Platform.Name},
		"platformInstance": map[string]any{
			"id": item.PlatformInstance.ID, "name": item.PlatformInstance.Name,
		},
		"defaultCore": map[string]any{"id": item.DefaultCore.ID, "name": item.DefaultCore.Name},
		"status":      item.Status, "version": item.Version,
		"createdAtMs": item.CreatedAtMS, "updatedAtMs": item.UpdatedAtMS,
		"lastPlayedAtMs": gameListInt(item.LastPlayedAtMS),
		"coverUrl":       gameListAssetURL(item.CoverAssetID),
	}
	if includeAdminProjection {
		result["releaseYear"] = gameListInt(item.ReleaseYear)
		result["metadataComplete"] = item.MetadataComplete
		result["runtimeStatus"] = gameListString(item.RuntimeStatus)
	}
	return result
}

func projectGameListFacets(facets gamelistservice.Facets) map[string]any {
	project := func(items []gamelistservice.Facet, includePlatform bool) []map[string]any {
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			value := map[string]any{"id": item.ID, "name": item.Name, "count": item.Count}
			if includePlatform {
				value["platformId"] = item.PlatformID
			}
			result = append(result, value)
		}
		return result
	}
	return map[string]any{
		"totalCount":        facets.TotalCount,
		"platforms":         project(facets.Platforms, false),
		"platformInstances": project(facets.PlatformInstances, true),
		"tags":              project(facets.Tags, false),
	}
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) gameList(writer http.ResponseWriter, request *http.Request, includeDeleted bool) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	values := request.URL.Query()
	filters, err := parseGameListFilters(values, includeDeleted)
	if err != nil {
		writeGameListFilterError(writer, request, err)
		return
	}
	sortCode, err := gameListSortCode(values.Get("sort"), includeDeleted)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏排序无效", map[string]any{})
		return
	}
	operationID := gameListOperationID(includeDeleted)
	filterDigest := cursor.FilterDigest(map[string]any{
		"principalId":        principal.UserID,
		"q":                  filters.NormalizedQ,
		"tagId":              values.Get("tagId"),
		"platformId":         values.Get("platformId"),
		"platformInstanceId": values.Get("platformInstanceId"),
		"status":             values.Get("status"),
		"sort":               sortCode,
	})
	pageCursor, err := server.applyGameListCursor(
		values.Get("cursor"), operationID, filterDigest, sortCode,
	)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏数量限制无效", map[string]any{})
			return
		}
	}
	result, err := server.gameListService.List(request.Context(), gamelistservice.ListRequest{
		ProfileID: principal.ProfileID, IncludeDeleted: includeDeleted,
		Filters: filters.Filters, Sort: sortCode, Cursor: pageCursor, Limit: limit,
		IncludeFacets: !includeDeleted && values.Get("cursor") == "",
	})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, projectGameListItem(item, includeDeleted))
	}
	if err := server.projectGameListAssociations(
		request.Context(), principal.ProfileID, items, includeDeleted,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if result.NextCursor != nil {
		nextCursor, err = server.cursors.Encode(cursor.Payload{
			OperationID: operationID, FilterDigest: filterDigest, SortCode: sortCode,
			SortValues: result.NextCursor.SortValues, ID: result.NextCursor.ID,
		})
		if err != nil {
			server.databaseError(writer, request, fmt.Errorf("encode game cursor: %w", err))
			return
		}
	}
	response := map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor,
	}
	if !includeDeleted && values.Get("cursor") == "" {
		response["filteredCount"] = result.FilteredCount
		response["facets"] = projectGameListFacets(result.Facets)
	}
	writeJSON(writer, http.StatusOK, response)
}

func gameListOperationID(includeDeleted bool) string {
	if includeDeleted {
		return "getAdminGames"
	}
	return "getGames"
}

func (server *Server) projectGameListFavorites(
	ctx context.Context,
	profileID string,
	items []map[string]any,
) error {
	gameIDs := make([]string, 0, len(items))
	for _, item := range items {
		gameID, _ := item["gameId"].(string)
		gameIDs = append(gameIDs, gameID)
	}
	references, err := server.favoriteService.References(ctx, profileID, gameIDs)
	if err != nil {
		return fmt.Errorf("project game list favorites: %w", err)
	}
	for _, item := range items {
		gameID, _ := item["gameId"].(string)
		if favorite, exists := references[gameID]; exists {
			item["favorite"] = favorite
		} else {
			item["favorite"] = nil
		}
	}
	return nil
}

func (server *Server) projectGameListTags(ctx context.Context, items []map[string]any) error {
	return projectMapTags(ctx, items, "gameId", server.tagService.References)
}

func (server *Server) projectGameListAssociations(
	ctx context.Context,
	profileID string,
	items []map[string]any,
	includeDeleted bool,
) error {
	if err := server.projectGameListTags(ctx, items); err != nil {
		return err
	}
	if includeDeleted {
		return nil
	}
	return server.projectGameListFavorites(ctx, profileID, items)
}
