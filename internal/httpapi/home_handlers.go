package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/favorites"
	"retrom/internal/tagging"
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

func scanRecentGame(scanner rowScanner) (recentGameProjection, error) {
	var game recentGameProjection
	var platformID, platformName, instanceID, instanceName string
	var coverAssetID sql.NullString
	if err := scanner.Scan(&game.GameID, &game.Title, &platformID, &platformName, &instanceID, &instanceName,
		&game.LastPlayedAtMS, &game.ActiveDurationMS, &game.SessionCount, &game.Status, &coverAssetID); err != nil {
		return recentGameProjection{}, fmt.Errorf("scan recent game: %w", err)
	}
	game.Platform = map[string]any{"id": platformID, "name": platformName}
	game.PlatformInstance = map[string]any{"id": instanceID, "name": instanceName}
	game.CoverURL = gameCoverURL(coverAssetID)
	game.Availability = game.Status
	return game, nil
}

func scanLatestGame(scanner rowScanner) (latestGameProjection, error) {
	var game latestGameProjection
	var platformID, platformName, instanceID, instanceName string
	var coverAssetID sql.NullString
	if err := scanner.Scan(&game.GameID, &game.Title, &platformID, &platformName, &instanceID, &instanceName,
		&game.CreatedAtMS, &coverAssetID); err != nil {
		return latestGameProjection{}, fmt.Errorf("scan latest game: %w", err)
	}
	game.Platform = map[string]any{"id": platformID, "name": platformName}
	game.PlatformInstance = map[string]any{"id": instanceID, "name": instanceName}
	game.CoverURL = gameCoverURL(coverAssetID)
	return game, nil
}

func (server *Server) projectRecentGameTags(ctx context.Context, games []recentGameProjection) error {
	gameIDs := make([]string, 0, len(games))
	for _, game := range games {
		gameIDs = append(gameIDs, game.GameID)
	}
	references, err := server.tagService.References(ctx, gameIDs)
	if err != nil {
		return fmt.Errorf("project recent game tags: %w", err)
	}
	for index := range games {
		games[index].Tags = references[games[index].GameID]
		if games[index].Tags == nil {
			games[index].Tags = []tagging.Reference{}
		}
	}
	return nil
}

func (server *Server) projectLatestGameTags(ctx context.Context, games []latestGameProjection) error {
	gameIDs := make([]string, 0, len(games))
	for _, game := range games {
		gameIDs = append(gameIDs, game.GameID)
	}
	references, err := server.tagService.References(ctx, gameIDs)
	if err != nil {
		return fmt.Errorf("project latest game tags: %w", err)
	}
	for index := range games {
		games[index].Tags = references[games[index].GameID]
		if games[index].Tags == nil {
			games[index].Tags = []tagging.Reference{}
		}
	}
	return nil
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

// The dashboard aggregates documented counters in one consistent response snapshot.
func (server *Server) home(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	var gameCount, saveCount, reviewCount, activeDurationMS int64
	if err := server.database.QueryRowContext(
		request.Context(),
		`SELECT count(*)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1`,
	).Scan(
		&gameCount,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.database.QueryRowContext(
		request.Context(),
		`SELECT count(*)
FROM save_states s
JOIN save_state_runtime_compatibility runtime_compatibility
  ON runtime_compatibility.save_state_id=s.id AND runtime_compatibility.status='AVAILABLE'
JOIN games g ON g.id=s.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE s.deleted_at_ms IS NULL
AND s.profile_id=?
AND g.status='PUBLISHED'
AND pi.enabled=1`,
		principal.ProfileID,
	).Scan(
		&saveCount,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.database.QueryRowContext(
		request.Context(),
		`SELECT count(*)
FROM import_items item
WHERE item.state='REVIEW_PENDING'
AND (
 item.review_handoff_kind='DIRECT'
 OR EXISTS (
  SELECT 1
  FROM emulationstation_import_items source
  WHERE source.library_import_item_id=item.id
  AND source.execution_state='REVIEW_PENDING'
 )
)`,
	).Scan(
		&reviewCount,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.database.QueryRowContext(
		request.Context(),
		`SELECT COALESCE(sum(ps.active_duration_ms),0)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
AND ps.profile_id=?`,
		principal.ProfileID,
	).Scan(
		&activeDurationMS,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	recentGames, err := server.homeRecentGames(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	recentSaves, err := server.homeRecentSaves(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	latestGames, err := server.homeLatestGames(request.Context())
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	featuredGame, err := server.homeFeaturedGame(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	platforms, quickPlatforms, err := server.homePlatforms(request.Context(), principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"library":        map[string]any{"gameCount": gameCount, "saveStateCount": saveCount},
		"imports":        map[string]any{"reviewPendingCount": reviewCount},
		"play":           map[string]any{"activeDurationMs": activeDurationMS},
		"featuredGame":   featuredGame.Value,
		"latestGames":    latestGames,
		"recentGames":    recentGames,
		"recentSaves":    recentSaves,
		"platforms":      platforms,
		"quickPlatforms": quickPlatforms,
	})
}

func (server *Server) homeRecentSaves(ctx context.Context, profileID string) ([]map[string]any, error) {
	saveRows, err := server.database.QueryContext(ctx, `
SELECT s.id,
s.game_id,
m.title,
s.name,
s.created_at_ms,
s.active_duration_ms,
s.disc_index
FROM save_states s
JOIN save_state_runtime_compatibility runtime_compatibility
  ON runtime_compatibility.save_state_id=s.id AND runtime_compatibility.status='AVAILABLE'
JOIN games g ON g.id=s.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE s.deleted_at_ms IS NULL
AND s.profile_id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
ORDER BY s.created_at_ms DESC,
s.id DESC LIMIT 3
`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query recent saves: %w", err)
	}
	defer func() { cleanup.Error("close", saveRows.Close()) }()
	recentSaves := make([]map[string]any, 0, 3)
	for saveRows.Next() {
		var saveID, gameID, title, name string
		var createdAtMS, activeDurationMS int64
		var discIndex sql.NullInt64
		if err := saveRows.Scan(
			&saveID, &gameID, &title, &name, &createdAtMS, &activeDurationMS, &discIndex,
		); err != nil {
			return nil, fmt.Errorf("scan recent save: %w", err)
		}
		recentSaves = append(
			recentSaves,
			map[string]any{
				"saveStateId":      saveID,
				"gameId":           gameID,
				"gameTitle":        title,
				"name":             name,
				"createdAtMs":      createdAtMS,
				"activeDurationMs": activeDurationMS,
				"discIndex":        nullableInteger(discIndex),
				"discLabel":        discLabel(discIndex),
				"screenshotUrl":    saveStateScreenshotURL(saveID),
			},
		)
	}
	if err := saveRows.Err(); err != nil {
		return nil, fmt.Errorf("scan recent saves: %w", err)
	}
	if err := projectMapTags(
		ctx, recentSaves, "gameId", server.tagService.References,
	); err != nil {
		return nil, err
	}
	return recentSaves, nil
}

func (server *Server) homeRecentGames(ctx context.Context, profileID string) ([]recentGameProjection, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
max(ps.started_at_ms),
sum(ps.active_duration_ms),
count(ps.id),
g.status,
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
AND ps.profile_id=?
GROUP BY g.id,m.title,p.id,p.name,pi.id,pi.name,g.status
ORDER BY max(ps.started_at_ms) DESC,g.id DESC
LIMIT 10
`, profileID)
	if err != nil {
		return nil, fmt.Errorf("home recent games: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	games := make([]recentGameProjection, 0, 10)
	for rows.Next() {
		game, scanErr := scanRecentGame(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("home recent game rows: %w", err)
	}
	if err := server.projectRecentGameTags(ctx, games); err != nil {
		return nil, err
	}
	return games, nil
}

func (server *Server) homeLatestGames(ctx context.Context) ([]latestGameProjection, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
g.created_at_ms,
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
ORDER BY g.created_at_ms DESC,g.id DESC
LIMIT 10
`)
	if err != nil {
		return nil, fmt.Errorf("home latest games: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	games := make([]latestGameProjection, 0, 10)
	for rows.Next() {
		game, scanErr := scanLatestGame(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("home latest game rows: %w", err)
	}
	if err := server.projectLatestGameTags(ctx, games); err != nil {
		return nil, err
	}
	return games, nil
}

type homeFeaturedResult struct {
	Value map[string]any
}

type homeSessionSave struct {
	value any
}

func (server *Server) featuredSessionSave(
	ctx context.Context,
	launchID, profileID string,
) (homeSessionSave, error) {
	var saveID string
	var createdAtMS, activeDurationMS int64
	var discIndex sql.NullInt64
	err := server.database.QueryRowContext(ctx, `
SELECT save.id,save.created_at_ms,save.active_duration_ms,save.disc_index
FROM save_states save
JOIN save_state_runtime_compatibility compatibility
  ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
WHERE save.source_launch_session_id=? AND save.profile_id=? AND save.deleted_at_ms IS NULL
ORDER BY save.created_at_ms DESC,save.id DESC
LIMIT 1
	`, launchID, profileID).Scan(&saveID, &createdAtMS, &activeDurationMS, &discIndex)
	if errors.Is(err, sql.ErrNoRows) {
		return homeSessionSave{}, nil
	}
	if err != nil {
		return homeSessionSave{}, fmt.Errorf("home featured session save: %w", err)
	}
	return homeSessionSave{value: map[string]any{
		"saveStateId": saveID, "createdAtMs": createdAtMS, "activeDurationMs": activeDurationMS,
		"discIndex": nullableInteger(discIndex), "discLabel": discLabel(discIndex),
		"screenshotUrl": saveStateScreenshotURL(saveID),
	}}, nil
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

func (server *Server) homeFeaturedGame(ctx context.Context, profileID string) (homeFeaturedResult, error) {
	var launchID, gameID, title, platformID, platformName, instanceID, instanceName string
	var lastPlayedAtMS, activeDurationMS, sessionCount int64
	var coverAssetID sql.NullString
	err := server.database.QueryRowContext(ctx, `
SELECT ps.launch_session_id,
g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
ps.started_at_ms,
(SELECT COALESCE(sum(all_sessions.active_duration_ms),0)
 FROM play_sessions all_sessions
 WHERE all_sessions.game_id=g.id AND all_sessions.profile_id=?),
(SELECT count(*) FROM play_sessions all_sessions WHERE all_sessions.game_id=g.id AND all_sessions.profile_id=?),
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED'
AND pi.enabled=1
AND ps.profile_id=?
ORDER BY ps.started_at_ms DESC,ps.id DESC
LIMIT 1
`, profileID, profileID, profileID).Scan(
		&launchID, &gameID, &title, &platformID, &platformName, &instanceID, &instanceName,
		&lastPlayedAtMS, &activeDurationMS, &sessionCount, &coverAssetID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return homeFeaturedResult{}, nil
	}
	if err != nil {
		return homeFeaturedResult{}, fmt.Errorf("home featured game: %w", err)
	}
	var saveCount int64
	if err := server.database.QueryRowContext(ctx, `
SELECT count(*) FROM save_states save
JOIN save_state_runtime_compatibility compatibility
  ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
WHERE save.game_id=? AND save.profile_id=? AND save.deleted_at_ms IS NULL
`, gameID, profileID).Scan(&saveCount); err != nil {
		return homeFeaturedResult{}, fmt.Errorf("home featured save count: %w", err)
	}
	lastSessionSave, err := server.featuredSessionSave(ctx, launchID, profileID)
	if err != nil {
		return homeFeaturedResult{}, err
	}
	tags, err := server.activeGameTags(ctx, gameID)
	if err != nil {
		return homeFeaturedResult{}, err
	}
	return homeFeaturedResult{Value: map[string]any{
		"gameId": gameID, "title": title,
		"platform":         map[string]any{"id": platformID, "name": platformName},
		"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
		"lastPlayedAtMs":   lastPlayedAtMS, "activeDurationMs": activeDurationMS,
		"sessionCount": sessionCount, "coverUrl": gameCoverURL(coverAssetID),
		"hasSaveStates": saveCount > 0, "lastSessionSave": lastSessionSave.value, "tags": tags,
	}}, nil
}

type homePlatform struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GameCount int64  `json:"gameCount"`
	PlayCount int64  `json:"playCount"`
}

func (server *Server) homePlatforms(ctx context.Context, profileID string) ([]homePlatform, []homePlatform, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT p.id,p.name,count(DISTINCT g.id),count(ps.id)
FROM platforms p
LEFT JOIN platform_instances pi ON pi.platform_id=p.id AND pi.enabled=1 AND pi.deleted_at_ms IS NULL
LEFT JOIN games g ON g.platform_instance_id=pi.id AND g.status='PUBLISHED'
LEFT JOIN play_sessions ps ON ps.game_id=g.id AND ps.profile_id=?
WHERE EXISTS (SELECT 1 FROM platform_cores pc WHERE pc.platform_id=p.id AND pc.enabled=1)
GROUP BY p.id,p.name
ORDER BY p.name COLLATE NOCASE,p.id
`, profileID)
	if err != nil {
		return nil, nil, fmt.Errorf("home platforms: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	platforms := make([]homePlatform, 0, 8)
	for rows.Next() {
		var platform homePlatform
		if err := rows.Scan(&platform.ID, &platform.Name, &platform.GameCount, &platform.PlayCount); err != nil {
			return nil, nil, fmt.Errorf("home platform row: %w", err)
		}
		platforms = append(platforms, platform)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("home platform rows: %w", err)
	}
	quickPlatforms := append([]homePlatform(nil), platforms...)
	sort.Slice(quickPlatforms, func(left, right int) bool {
		if quickPlatforms[left].PlayCount != quickPlatforms[right].PlayCount {
			return quickPlatforms[left].PlayCount > quickPlatforms[right].PlayCount
		}
		if quickPlatforms[left].Name != quickPlatforms[right].Name {
			return quickPlatforms[left].Name < quickPlatforms[right].Name
		}
		return quickPlatforms[left].ID < quickPlatforms[right].ID
	})
	if len(quickPlatforms) > 4 {
		quickPlatforms = quickPlatforms[:4]
	}
	return platforms, quickPlatforms, nil
}

// recentGames returns every visible game with play history, ordered by the
// most recently started play session. This is a game projection rather than a
// session log, so one game always occupies one row regardless of play count.
func (server *Server) recentGames(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	rows, err := server.database.QueryContext(request.Context(), `
SELECT g.id,
m.title,
p.id,
p.name,
pi.id,
pi.name,
max(ps.started_at_ms),
sum(ps.active_duration_ms),
count(ps.id),
g.status,
(SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.metadata_revision_id=g.current_metadata_revision_id
 AND a.kind='COVER'
 ORDER BY a.ordinal,a.id
 LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status IN ('PUBLISHED','DELETED')
AND (g.status='DELETED' OR pi.enabled=1)
AND ps.profile_id=?
GROUP BY g.id,m.title,p.id,p.name,pi.id,pi.name,g.status
ORDER BY max(ps.started_at_ms) DESC,g.id DESC
`, principal.ProfileID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]recentGameProjection, 0)
	for rows.Next() {
		game, err := scanRecentGame(rows)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		items = append(items, game)
	}
	if err := rows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.projectRecentGameTags(request.Context(), items); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"generatedAtMs": server.now().UnixMilli(), "items": items})
}
