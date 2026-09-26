package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/service/favorites"
	homeservice "retrom/internal/service/home"
	"retrom/internal/service/tagging"
)

type recentGameProjection struct {
	GameID           string              `json:"gameId"`
	Title            string              `json:"title"`
	Platform         map[string]any      `json:"platform"`
	PlatformInstance map[string]any      `json:"platformInstance"`
	LastPlayedAtMS   int64               `json:"lastPlayedAtMs"`
	ActiveDurationMS int64               `json:"activeDurationMs"`
	SessionCount     int64               `json:"sessionCount"`
	CoverURL         any                 `json:"coverUrl"`
	Status           string              `json:"status"`
	Availability     string              `json:"availability"`
	Tags             []tagging.Reference `json:"tags"`
}

type latestGameProjection struct {
	GameID           string              `json:"gameId"`
	Title            string              `json:"title"`
	Platform         map[string]any      `json:"platform"`
	PlatformInstance map[string]any      `json:"platformInstance"`
	CreatedAtMS      int64               `json:"createdAtMs"`
	CoverURL         any                 `json:"coverUrl"`
	Tags             []tagging.Reference `json:"tags"`
}

type homePlatform struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GameCount int64  `json:"gameCount"`
	PlayCount int64  `json:"playCount"`
}

func homeAssetURL(assetID *string) any {
	if assetID == nil {
		return nil
	}
	return "/content/assets/" + *assetID
}

func projectHomeRecentGame(item homeservice.RecentGame) recentGameProjection {
	return recentGameProjection{
		GameID: item.GameID, Title: item.Title,
		Platform:         map[string]any{"id": item.Platform.ID, "name": item.Platform.Name},
		PlatformInstance: map[string]any{"id": item.PlatformInstance.ID, "name": item.PlatformInstance.Name},
		LastPlayedAtMS:   item.LastPlayedAtMS, ActiveDurationMS: item.ActiveDurationMS,
		SessionCount: item.SessionCount, CoverURL: homeAssetURL(item.CoverAssetID),
		Status: item.Status, Availability: item.Availability, Tags: item.Tags,
	}
}

func projectHomeLatestGame(item homeservice.LatestGame) latestGameProjection {
	return latestGameProjection{
		GameID: item.GameID, Title: item.Title,
		Platform:         map[string]any{"id": item.Platform.ID, "name": item.Platform.Name},
		PlatformInstance: map[string]any{"id": item.PlatformInstance.ID, "name": item.PlatformInstance.Name},
		CreatedAtMS:      item.CreatedAtMS, CoverURL: homeAssetURL(item.CoverAssetID), Tags: item.Tags,
	}
}

func projectHomeRecentSave(item homeservice.RecentSave) map[string]any {
	return map[string]any{
		"saveStateId": item.SaveStateID, "gameId": item.GameID, "gameTitle": item.GameTitle,
		"name": item.Name, "createdAtMs": item.CreatedAtMS,
		"lastSyncedAtMs":   homeNullableInteger(item.LastSyncedAtMS),
		"activeDurationMs": item.ActiveDurationMS, "discIndex": homeNullableInteger(item.DiscIndex),
		"discLabel":     homeDiscLabel(item.DiscIndex),
		"screenshotUrl": optionalSaveScreenshotURL(item.SaveStateID, item.HasScreenshot),
		"tags":          item.Tags,
	}
}

func projectHomeFeaturedGame(item *homeservice.FeaturedGame) map[string]any {
	if item == nil {
		return nil
	}
	var lastSessionSave any
	if item.LastSessionSave != nil {
		lastSessionSave = map[string]any{
			"saveStateId":      item.LastSessionSave.SaveStateID,
			"createdAtMs":      item.LastSessionSave.CreatedAtMS,
			"activeDurationMs": item.LastSessionSave.ActiveDurationMS,
			"discIndex":        homeNullableInteger(item.LastSessionSave.DiscIndex),
			"discLabel":        homeDiscLabel(item.LastSessionSave.DiscIndex),
			"screenshotUrl": optionalSaveScreenshotURL(
				item.LastSessionSave.SaveStateID, item.LastSessionSave.HasScreenshot,
			),
		}
	}
	return map[string]any{
		"gameId": item.GameID, "title": item.Title, "description": item.Description,
		"platform":         map[string]any{"id": item.Platform.ID, "name": item.Platform.Name},
		"platformInstance": map[string]any{"id": item.PlatformInstance.ID, "name": item.PlatformInstance.Name},
		"lastPlayedAtMs":   item.LastPlayedAtMS, "activeDurationMs": item.ActiveDurationMS,
		"sessionCount": item.SessionCount, "coverUrl": homeAssetURL(item.CoverAssetID),
		"hasSaveStates": item.SaveCount > 0, "lastSessionSave": lastSessionSave,
		"defaultDosEntry": homeNullableString(item.DefaultDOSEntry), "tags": item.Tags,
	}
}

func homeNullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func homeNullableInteger(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func homeDiscLabel(value *int64) any {
	if value == nil {
		return nil
	}
	return fmt.Sprintf("光盘 %d", *value+1)
}

// The dashboard aggregates documented counters in one consistent response snapshot.
func (server *Server) home(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	data, err := server.homeService.Dashboard(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	recentGames := make([]recentGameProjection, 0, len(data.RecentGames))
	for _, item := range data.RecentGames {
		recentGames = append(recentGames, projectHomeRecentGame(item))
	}
	latestGames := make([]latestGameProjection, 0, len(data.LatestGames))
	for _, item := range data.LatestGames {
		latestGames = append(latestGames, projectHomeLatestGame(item))
	}
	recentSaves := make([]map[string]any, 0, len(data.RecentSaves))
	for _, item := range data.RecentSaves {
		recentSaves = append(recentSaves, projectHomeRecentSave(item))
	}
	platforms := make([]homePlatform, 0, len(data.Platforms))
	for _, item := range data.Platforms {
		platforms = append(platforms, homePlatform{
			ID: item.ID, Name: item.Name, GameCount: item.GameCount, PlayCount: item.PlayCount,
		})
	}
	quickPlatforms := make([]homePlatform, 0, len(data.QuickPlatforms))
	for _, item := range data.QuickPlatforms {
		quickPlatforms = append(quickPlatforms, homePlatform{
			ID: item.ID, Name: item.Name, GameCount: item.GameCount, PlayCount: item.PlayCount,
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"library":      map[string]any{"gameCount": data.Summary.GameCount, "saveStateCount": data.Summary.SaveCount},
		"imports":      map[string]any{"reviewPendingCount": data.Summary.ReviewCount},
		"play":         map[string]any{"activeDurationMs": data.Summary.ActiveDurationMS},
		"featuredGame": projectHomeFeaturedGame(data.FeaturedGame), "latestGames": latestGames,
		"recentGames": recentGames, "recentSaves": recentSaves, "platforms": platforms,
		"quickPlatforms": quickPlatforms,
	})
}

type tagReferenceLoader func(context.Context, []string) (map[string][]tagging.Reference, error)

func projectMapTags(
	ctx context.Context,
	items []map[string]any,
	idKey string,
	load tagReferenceLoader,
) error {
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		itemID, ok := item[idKey].(string)
		if !ok {
			return fmt.Errorf("%w: %s", errTagProjectionType, idKey)
		}
		itemIDs = append(itemIDs, itemID)
	}
	references, err := load(ctx, itemIDs)
	if err != nil {
		return fmt.Errorf("load tag projection for %s: %w", idKey, err)
	}
	for _, item := range items {
		itemID, ok := item[idKey].(string)
		if !ok {
			return fmt.Errorf("%w after loading: %s", errTagProjectionType, idKey)
		}
		item["tags"] = references[itemID]
		if item["tags"] == nil {
			item["tags"] = []tagging.Reference{}
		}
	}
	return nil
}

func (server *Server) activeGameTags(ctx context.Context, gameID string) ([]tagging.Reference, error) {
	references, err := server.tagService.References(ctx, []string{gameID})
	if err != nil {
		return nil, fmt.Errorf("project game tags: %w", err)
	}
	tags := references[gameID]
	if tags == nil {
		tags = []tagging.Reference{}
	}
	return tags, nil
}

func (server *Server) gameAssociations(
	ctx context.Context,
	profileID, gameID string,
) (*favorites.FavoriteReference, []tagging.Reference, error) {
	favorite, err := server.favoriteService.Reference(ctx, profileID, gameID)
	if err != nil {
		return nil, nil, fmt.Errorf("project game favorite: %w", err)
	}
	tags, err := server.activeGameTags(ctx, gameID)
	if err != nil {
		return nil, nil, err
	}
	return favorite, tags, nil
}

// recentGames returns every visible game with play history, ordered by the
// most recently started play session. This is a game projection rather than a
// session log, so one game always occupies one row regardless of play count.
func (server *Server) recentGames(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	games, err := server.homeService.RecentGames(request.Context(), principal.ProfileID, true)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items := make([]recentGameProjection, 0, len(games))
	for _, game := range games {
		items = append(items, projectHomeRecentGame(game))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"generatedAtMs": server.now().UnixMilli(), "items": items})
}
