package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/cursor"
	"retrom/internal/tagging"
)

func (server *Server) games(writer http.ResponseWriter, request *http.Request) {
	server.gameList(writer, request, false)
}

// Method dispatch and nullable detail projections stay at the route protocol boundary.
func (server *Server) game(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	gameID := request.PathValue("gameId")
	var title, description, developer, publisher, genre string
	var platformID, platformName, instanceID, instanceName string
	var players, releaseYear sql.NullInt64
	var coverAssetID, videoAssetID sql.NullString
	var version, updatedAtMS, activeDurationMS int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT g.title,
g.description,
g.developer,
g.publisher,
g.genre,
g.players,
g.release_year,
p.id,
p.name,
pi.id,
pi.name,
g.version,
g.updated_at_ms,
(SELECT a.id
FROM game_assets a
WHERE a.game_id=g.id
AND a.game_id=g.id
AND a.kind='COVER'
ORDER BY a.ordinal,
a.id
LIMIT 1),
(SELECT a.id
FROM game_assets a
WHERE a.game_id=g.id
AND a.game_id=g.id
AND a.kind='VIDEO'
AND a.ordinal=0
ORDER BY a.id
LIMIT 1),
COALESCE((SELECT SUM(active_duration_ms)
FROM play_sessions ps
WHERE ps.game_id=g.id
AND ps.profile_id=?),
0)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
`, principal.ProfileID, gameID).
		Scan(&title, &description, &developer, &publisher, &genre, &players, &releaseYear,
			&platformID, &platformName, &instanceID, &instanceName,
			&version, &updatedAtMS, &coverAssetID, &videoAssetID, &activeDurationMS,
		)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(writer, request, http.StatusNotFound, "GAME_NOT_FOUND", "游戏不存在", map[string]any{})
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	coreOptions, err := server.gameCoreOptions(request.Context(), gameID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	dosEntries := make([]map[string]any, 0)
	dosRows, err := server.database.QueryContext(
		request.Context(),
		`
SELECT normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe
FROM dos_entries
WHERE game_id=?
ORDER BY rank,
normalized_path
`,
		gameID,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", dosRows.Close()) }()
	for dosRows.Next() {
		var normalizedPath, originalPath, kind string
		var rank int64
		var enabled, directLaunchSafe int
		if err := dosRows.Scan(&normalizedPath, &originalPath, &kind, &rank, &enabled, &directLaunchSafe); err != nil {
			server.databaseError(writer, request, err)
			return
		}
		dosEntries = append(dosEntries, map[string]any{
			"path": normalizedPath, "originalPath": originalPath, "kind": kind, "rank": rank,
			"enabled": enabled == 1, "directLaunchSafe": directLaunchSafe == 1,
		})
	}
	if err := dosRows.Err(); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defaultDOSEntry, err := server.gameDefaultDOSEntry(request.Context(), gameID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var saveStateCount int64
	if err := server.database.QueryRowContext(request.Context(), `
SELECT count(*)
FROM save_states save
JOIN save_state_runtime_compatibility compatibility
  ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
WHERE save.game_id=?
AND save.profile_id=?
AND save.deleted_at_ms IS NULL
`, gameID, principal.ProfileID).Scan(&saveStateCount); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	saveStates, err := server.gameRecentSaveStates(request.Context(), gameID, principal.ProfileID)
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
		"gameId": gameID, "title": title, "description": description, "developer": developer, "publisher": publisher,
		"genre": genre, "players": nullableInteger(players), "releaseYear": nullableInteger(releaseYear),
		"platform":         map[string]any{"id": platformID, "name": platformName},
		"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
		"version":          version, "updatedAtMs": updatedAtMS,
		"coverUrl": gameCoverURL(
			coverAssetID,
		), "videoUrl": gameCoverURL(videoAssetID), "activeDurationMs": activeDurationMS, "coreOptions": coreOptions,
		"dosEntries": dosEntries, "defaultDosEntry": nullableString(defaultDOSEntry),
		"saveStateCount": saveStateCount, "saveStates": saveStates,
		"favorite": favorite, "tags": tags,
	})
}

func (server *Server) gameRecentSaveStates(
	ctx context.Context,
	gameID, profileID string,
) ([]map[string]any, error) {
	saveRows, err := server.database.QueryContext(ctx, `
SELECT s.id,
s.name,
s.created_at_ms,
source_launch.core_id,
c.name,
s.disc_index,
s.screenshot_blob_id IS NOT NULL
FROM save_states s
JOIN launch_sessions source_launch ON source_launch.id=s.source_launch_session_id
JOIN cores c ON c.id=source_launch.core_id
JOIN save_state_runtime_compatibility compatibility
  ON compatibility.save_state_id=s.id AND compatibility.status='AVAILABLE'
WHERE s.game_id=?
AND s.profile_id=?
AND s.deleted_at_ms IS NULL
ORDER BY s.created_at_ms DESC,
s.id DESC
LIMIT 8
`, gameID, profileID)
	if err != nil {
		return nil, fmt.Errorf("query recent game saves: %w", err)
	}
	defer func() { cleanup.Error("close", saveRows.Close()) }()
	saveStates := make([]map[string]any, 0)
	for saveRows.Next() {
		var saveID, saveName, coreID, coreName string
		var createdAtMS int64
		var discIndex sql.NullInt64
		var hasScreenshot bool
		if err := saveRows.Scan(
			&saveID, &saveName, &createdAtMS, &coreID, &coreName, &discIndex, &hasScreenshot,
		); err != nil {
			return nil, fmt.Errorf("scan recent game save: %w", err)
		}
		saveStates = append(saveStates, map[string]any{
			"saveStateId": saveID, "name": saveName, "createdAtMs": createdAtMS,
			"discIndex": nullableInteger(discIndex), "discLabel": discLabel(discIndex),
			"screenshotUrl": optionalSaveScreenshotURL(saveID, hasScreenshot),
			"core":          map[string]any{"id": coreID, "name": coreName},
		})
	}
	if err := saveRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent game saves: %w", err)
	}
	return saveStates, nil
}

func (server *Server) gameDefaultDOSEntry(ctx context.Context, gameID string) (sql.NullString, error) {
	var entry sql.NullString
	err := server.database.QueryRowContext(ctx, `
SELECT variant.default_dos_entry
FROM game_variants variant
WHERE variant.game_id=? AND variant.core_id='dosbox_pure'
`, gameID).Scan(&entry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, fmt.Errorf("query default DOS entry: %w", err)
	}
	return entry, nil
}

func (server *Server) gameCoreOptions(ctx context.Context, gameID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT c.id,
c.name,
COALESCE(json_extract(bound_target.capabilities_json,'$.requiresThreads'),
 (SELECT max(json_extract(candidate_target.capabilities_json,'$.requiresThreads'))
  FROM runtime_target_bindings candidate
  JOIN runtime_binding_platforms candidate_platform ON candidate_platform.binding_id=candidate.binding_id
   AND candidate_platform.platform_id=pi.platform_id AND candidate_platform.core_id=c.id
  JOIN runtime_targets candidate_target ON candidate_target.provider_id=candidate.provider_id
   AND candidate_target.target_id=candidate.target_id
  WHERE candidate.core_id=c.id AND candidate.launch_policy<>'DISABLED'),0),
pi.default_core_id,
v.id,
v.provider_id,
v.target_id,
v.dat_version_id,
v.status,
v.compatibility_code
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platform_cores pc ON pc.platform_id=pi.platform_id
AND pc.enabled=1
JOIN cores c ON c.id=pc.core_id
AND c.enabled=1
LEFT JOIN game_variants v ON v.game_id=g.id
AND (v.core_id=c.id OR pi.platform_id='rpgmaker')
LEFT JOIN runtime_targets bound_target ON bound_target.provider_id=v.provider_id AND bound_target.target_id=v.target_id
WHERE g.id=?
ORDER BY c.name,
c.id
`,
		gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("query game core options: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	coreOptions := make([]map[string]any, 0)
	for rows.Next() {
		var coreID, coreName, defaultCoreID string
		var requiresThreads int
		var variantID, providerID, targetID sql.NullString
		var datVersionID, status, compatibility sql.NullString
		if err := rows.Scan(
			&coreID,
			&coreName,
			&requiresThreads,
			&defaultCoreID,
			&variantID,
			&providerID,
			&targetID,
			&datVersionID,
			&status,
			&compatibility,
		); err != nil {
			return nil, fmt.Errorf("scan game core option: %w", err)
		}
		projectedStatus := "NEEDS_VALIDATION"
		var reasons []map[string]any
		switch {
		case variantID.Valid && status.String == "READY":
			projectedStatus = "READY"
			reasons = []map[string]any{}
		case variantID.Valid && status.String == "BLOCKED":
			projectedStatus = "DEPENDENCY_MISSING"
			reasons = []map[string]any{{"code": compatibility.String, "level": "BLOCKING"}}
		case variantID.Valid:
			projectedStatus = "INCOMPATIBLE"
			reasons = []map[string]any{{"code": compatibility.String, "level": "BLOCKING"}}
		default:
			reasons = []map[string]any{{"code": "VARIANT_VALIDATION_REQUIRED", "level": "INFO"}}
		}
		coreOptions = append(coreOptions, map[string]any{
			"coreId": coreID, "name": coreName, "isDefault": coreID == defaultCoreID, "status": projectedStatus,
			"revalidationStatus": "NOT_REQUIRED", "variantId": nullableString(variantID),
			"providerId": nullableString(providerID), "targetId": nullableString(targetID),
			"datVersionId": nullableString(datVersionID), "revalidationJobId": nil,
			"requiresThreads": requiresThreads == 1, "reasons": reasons,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan game core options: %w", err)
	}
	return coreOptions, nil
}

func (server *Server) adminGames(writer http.ResponseWriter, request *http.Request) {
	server.gameList(writer, request, true)
}

func gameListVisibilityConditions(includeDisabled bool) []string {
	if includeDisabled {
		return nil
	}
	return []string{"pi.enabled=1"}
}

type gameListFilters struct {
	Conditions  []string
	Arguments   []any
	NormalizedQ string
}

type gameListFacet struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PlatformID string `json:"platformId,omitempty"`
	Count      int64  `json:"count"`
}

type gameListFacets struct {
	TotalCount        int64           `json:"totalCount"`
	Platforms         []gameListFacet `json:"platforms"`
	PlatformInstances []gameListFacet `json:"platformInstances"`
	Tags              []gameListFacet `json:"tags"`
}

func queryGameListFacetRows(
	ctx context.Context,
	database *sql.DB,
	query string,
	suffix string,
	includePlatform bool,
) ([]gameListFacet, error) {
	visible := []string{"g.status='PUBLISHED'", "pi.enabled=1"}
	rows, err := database.QueryContext(ctx, queryWithConditions(query, visible, suffix))
	if err != nil {
		return nil, fmt.Errorf("query game facets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]gameListFacet, 0)
	for rows.Next() {
		var item gameListFacet
		if includePlatform {
			err = rows.Scan(&item.ID, &item.Name, &item.PlatformID, &item.Count)
		} else {
			err = rows.Scan(&item.ID, &item.Name, &item.Count)
		}
		if err != nil {
			return nil, fmt.Errorf("scan game facet: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game facets: %w", err)
	}
	return items, nil
}

func queryGameListFacets(
	ctx context.Context,
	database *sql.DB,
	filteredConditions []string,
	filteredArguments []any,
) (int64, gameListFacets, error) {
	baseFrom := `
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
`
	var filteredCount int64
	if err := database.QueryRowContext(
		ctx,
		queryWithConditions("SELECT count(*) "+baseFrom, filteredConditions, ""),
		filteredArguments...,
	).Scan(&filteredCount); err != nil {
		return 0, gameListFacets{}, fmt.Errorf("count filtered games: %w", err)
	}

	platforms, err := queryGameListFacetRows(
		ctx,
		database,
		"SELECT p.id,p.name,count(*) "+baseFrom,
		" GROUP BY p.id,p.name ORDER BY p.name,p.id",
		false,
	)
	if err != nil {
		return 0, gameListFacets{}, fmt.Errorf("list game platform facets: %w", err)
	}
	platformInstances, err := queryGameListFacetRows(
		ctx,
		database,
		"SELECT pi.id,pi.name,p.id,count(*) "+baseFrom,
		" GROUP BY pi.id,pi.name,p.id ORDER BY pi.name,pi.id",
		true,
	)
	if err != nil {
		return 0, gameListFacets{}, fmt.Errorf("list game directory facets: %w", err)
	}
	tagFrom := baseFrom + `
JOIN game_tags relation ON relation.game_id=g.id
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
`
	tags, err := queryGameListFacetRows(
		ctx,
		database,
		"SELECT tag.id,tag.name,count(*) "+tagFrom,
		" GROUP BY tag.id,tag.name ORDER BY tag.name,tag.id",
		false,
	)
	if err != nil {
		return 0, gameListFacets{}, fmt.Errorf("list game tag facets: %w", err)
	}
	facets := gameListFacets{Platforms: platforms, PlatformInstances: platformInstances, Tags: tags}
	for _, platform := range platforms {
		facets.TotalCount += platform.Count
	}
	return filteredCount, facets, nil
}

func parseGameListFilters(values url.Values, includeDeleted bool) (gameListFilters, error) {
	filters := gameListFilters{Conditions: gameListVisibilityConditions(includeDeleted)}
	status := values.Get("status")
	switch {
	case !includeDeleted || status == "PUBLISHED":
		filters.Conditions = append(filters.Conditions, "g.status='PUBLISHED'")
	case status == "DELETED":
		filters.Conditions = append(filters.Conditions, "g.status='DELETED'")
	case status != "" && status != "ALL":
		return gameListFilters{}, fmt.Errorf("%w: game status", errUnknownQuery)
	}
	filters.NormalizedQ = strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " "))
	if filters.NormalizedQ != "" {
		filters.Conditions = append(filters.Conditions, `(instr(g.search_text,?)>0 OR EXISTS(
SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id=g.id AND instr(tag.search_text,?)>0))`)
		filters.Arguments = append(filters.Arguments, filters.NormalizedQ, filters.NormalizedQ)
	}
	if tagID := values.Get("tagId"); tagID != "" {
		if !tagging.ValidID(tagID) {
			return gameListFilters{}, errInvalidGameTagFilter
		}
		filters.Conditions = append(filters.Conditions, `EXISTS(
SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id=g.id AND tag.id=?)`)
		filters.Arguments = append(filters.Arguments, tagID)
	}
	for _, filter := range []struct{ queryName, column string }{
		{"platformId", "p.id"}, {"platformInstanceId", "pi.id"},
	} {
		if value := values.Get(filter.queryName); value != "" {
			filters.Conditions = append(filters.Conditions, filter.column+"=?")
			filters.Arguments = append(filters.Arguments, value)
		}
	}
	return filters, nil
}

func writeGameListFilterError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, errInvalidGameTagFilter) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "标签筛选无效", map[string]any{})
		return
	}
	writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏状态筛选无效", map[string]any{})
}

func scanGameListItem(scanner rowScanner, includeAdminProjection bool) (map[string]any, error) {
	var id, title, platformID, platformName, instanceID, instanceName, defaultCoreID, defaultCoreName, status string
	var version, createdAtMS, updatedAtMS int64
	var lastPlayedAtMS, releaseYear sql.NullInt64
	var metadataComplete int64
	var runtimeStatus, coverAssetID sql.NullString
	if err := scanner.Scan(
		&id,
		&title,
		&platformID,
		&platformName,
		&instanceID,
		&instanceName,
		&defaultCoreID,
		&defaultCoreName,
		&status,
		&version,
		&createdAtMS,
		&updatedAtMS,
		&lastPlayedAtMS,
		&releaseYear,
		&metadataComplete,
		&runtimeStatus,
		&coverAssetID,
	); err != nil {
		return nil, fmt.Errorf("scan game list item: %w", err)
	}
	item := map[string]any{
		"gameId": id, "title": title, "platform": map[string]any{"id": platformID, "name": platformName},
		"platformInstance": map[string]any{"id": instanceID, "name": instanceName},
		"defaultCore":      map[string]any{"id": defaultCoreID, "name": defaultCoreName},
		"status":           status, "version": version, "createdAtMs": createdAtMS, "updatedAtMs": updatedAtMS,
		"lastPlayedAtMs": nullableInteger(lastPlayedAtMS), "coverUrl": gameCoverURL(coverAssetID),
	}
	if includeAdminProjection {
		item["releaseYear"] = nullableInteger(releaseYear)
		item["metadataComplete"] = metadataComplete == 1
		item["runtimeStatus"] = nullableString(runtimeStatus)
	}
	return item, nil
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

func scanGameListRows(rows *sql.Rows, includeDeleted bool, capacity int) ([]map[string]any, error) {
	items := make([]map[string]any, 0, capacity)
	for rows.Next() {
		item, err := scanGameListItem(rows, includeDeleted)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game list: %w", err)
	}
	return items, nil
}

func gameListSortCode(raw string, includeDeleted bool) (string, error) {
	if raw == "" {
		if includeDeleted {
			return "UPDATED_DESC", nil
		}
		return "RECENT_DESC", nil
	}
	switch raw {
	case "TITLE_ASC", "ADDED_DESC":
		return raw, nil
	case "RECENT_DESC":
		if !includeDeleted {
			return raw, nil
		}
	case "UPDATED_DESC":
		if includeDeleted {
			return raw, nil
		}
	}
	return "", errUnknownQuery
}

func gameListInteger(item map[string]any, key string, fallback int64) int64 {
	value, ok := item[key].(int64)
	if !ok {
		return fallback
	}
	return value
}

func appendGameListTitleCursor(payload cursor.Payload, conditions *[]string, arguments *[]any) error {
	if len(payload.SortValues) != 1 {
		return errInvalidCursorPayload
	}
	*conditions = append(*conditions, "(g.title>? OR (g.title=? AND g.id>?))")
	*arguments = append(*arguments, payload.SortValues[0], payload.SortValues[0], payload.ID)
	return nil
}

func appendGameListTimestampCursor(
	payload cursor.Payload,
	column string,
	conditions *[]string,
	arguments *[]any,
) error {
	if len(payload.SortValues) != 2 {
		return errInvalidCursorPayload
	}
	timestamp, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
	if err != nil {
		return errInvalidCursorPayload
	}
	*conditions = append(*conditions, fmt.Sprintf(
		"(%s<? OR (%s=? AND (g.title>? OR (g.title=? AND g.id>?))))", column, column,
	))
	*arguments = append(*arguments, timestamp, timestamp, payload.SortValues[1], payload.SortValues[1], payload.ID)
	return nil
}

func appendGameListRecentCursor(
	payload cursor.Payload,
	profileID string,
	conditions *[]string,
	arguments *[]any,
) error {
	if len(payload.SortValues) != 3 {
		return errInvalidCursorPayload
	}
	lastPlayed, playedErr := strconv.ParseInt(payload.SortValues[0], 10, 64)
	createdAt, createdErr := strconv.ParseInt(payload.SortValues[1], 10, 64)
	if playedErr != nil || createdErr != nil {
		return errInvalidCursorPayload
	}
	lastPlayedExpression := `COALESCE((SELECT max(ps_cursor.started_at_ms)
FROM play_sessions ps_cursor WHERE ps_cursor.game_id=g.id AND ps_cursor.profile_id=?),-1)`
	*conditions = append(*conditions, fmt.Sprintf(
		`(%s<? OR (%s=? AND (g.created_at_ms<? OR (g.created_at_ms=? AND (g.title>? OR (g.title=? AND g.id>?))))))`,
		lastPlayedExpression, lastPlayedExpression,
	))
	*arguments = append(*arguments,
		profileID, lastPlayed, profileID, lastPlayed,
		createdAt, createdAt, payload.SortValues[2], payload.SortValues[2], payload.ID,
	)
	return nil
}

func (server *Server) applyGameListCursor(
	token string,
	operationID string,
	filterDigest string,
	sortCode string,
	profileID string,
	conditions *[]string,
	arguments *[]any,
) error {
	if token == "" {
		return nil
	}
	payload, err := server.cursors.Decode(token, operationID, filterDigest, sortCode)
	if err != nil {
		return errInvalidCursorPayload
	}
	switch sortCode {
	case "TITLE_ASC":
		return appendGameListTitleCursor(payload, conditions, arguments)
	case "ADDED_DESC":
		return appendGameListTimestampCursor(payload, "g.created_at_ms", conditions, arguments)
	case "UPDATED_DESC":
		return appendGameListTimestampCursor(payload, "g.updated_at_ms", conditions, arguments)
	case "RECENT_DESC":
		return appendGameListRecentCursor(payload, profileID, conditions, arguments)
	default:
		return errInvalidCursorPayload
	}
}

func gameListCursorSortValues(item map[string]any, sortCode, title string) []string {
	switch sortCode {
	case "RECENT_DESC":
		return []string{
			strconv.FormatInt(gameListInteger(item, "lastPlayedAtMs", -1), 10),
			strconv.FormatInt(gameListInteger(item, "createdAtMs", 0), 10),
			title,
		}
	case "ADDED_DESC":
		return []string{strconv.FormatInt(gameListInteger(item, "createdAtMs", 0), 10), title}
	case "UPDATED_DESC":
		return []string{strconv.FormatInt(gameListInteger(item, "updatedAtMs", 0), 10), title}
	default:
		return []string{title}
	}
}

func (server *Server) encodeGameListNextCursor(
	items []map[string]any,
	limit int,
	operationID string,
	filterDigest string,
	sortCode string,
) ([]map[string]any, any, error) {
	if len(items) <= limit {
		return items, nil, nil
	}
	last := items[limit-1]
	lastTitle, titleOK := last["title"].(string)
	lastID, idOK := last["gameId"].(string)
	if !titleOK || !idOK {
		return nil, nil, errGamePagination
	}
	token, err := server.cursors.Encode(cursor.Payload{
		OperationID:  operationID,
		FilterDigest: filterDigest,
		SortCode:     sortCode,
		SortValues:   gameListCursorSortValues(last, sortCode, lastTitle),
		ID:           lastID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("encode game cursor: %w", err)
	}
	return items[:limit], token, nil
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) gameList(writer http.ResponseWriter, request *http.Request, includeDeleted bool) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	query := `
SELECT g.id,
 g.title,
 p.id,
 p.name,
 pi.id,
 pi.name,
 dc.id,
 dc.name,
 g.status,
 g.version,
 g.created_at_ms,
 g.updated_at_ms,
 (SELECT max(ps.started_at_ms) FROM play_sessions ps WHERE ps.game_id=g.id AND ps.profile_id=?) AS last_played_at_ms,
 g.release_year,
 CASE WHEN trim(g.description)<>''
 AND trim(g.developer)<>''
 AND trim(g.publisher)<>''
 AND trim(g.genre)<>''
 AND g.players IS NOT NULL
 AND g.release_year IS NOT NULL THEN 1 ELSE 0 END,
 (SELECT variant.status
 FROM game_variants variant
 WHERE variant.game_id=g.id
 AND variant.core_id=pi.default_core_id
 LIMIT 1),
 (SELECT a.id
 FROM game_assets a
 WHERE a.game_id=g.id
 AND a.kind='COVER'
 ORDER BY a.ordinal,
 a.id
 LIMIT 1)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
JOIN cores dc ON dc.id=pi.default_core_id
`
	values := request.URL.Query()
	filters, err := parseGameListFilters(values, includeDeleted)
	if err != nil {
		writeGameListFilterError(writer, request, err)
		return
	}
	conditions := filters.Conditions
	arguments := append([]any{principal.ProfileID}, filters.Arguments...)
	baseConditions := append([]string(nil), filters.Conditions...)
	baseArguments := append([]any(nil), filters.Arguments...)
	normalizedQ := filters.NormalizedQ
	sortCode, err := gameListSortCode(values.Get("sort"), includeDeleted)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "游戏排序无效", map[string]any{})
		return
	}
	operationID := gameListOperationID(includeDeleted)
	filterDigest := cursor.FilterDigest(
		map[string]any{
			"principalId":        principal.UserID,
			"q":                  normalizedQ,
			"tagId":              values.Get("tagId"),
			"platformId":         values.Get("platformId"),
			"platformInstanceId": values.Get("platformInstanceId"),
			"status":             values.Get("status"),
			"sort":               sortCode,
		},
	)
	if err := server.applyGameListCursor(
		values.Get("cursor"), operationID, filterDigest, sortCode, principal.ProfileID, &conditions, &arguments,
	); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	limit := 50
	if raw := values.Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	order := " ORDER BY g.title ASC,g.id ASC LIMIT ?"
	switch sortCode {
	case "RECENT_DESC":
		order = " ORDER BY last_played_at_ms DESC,g.created_at_ms DESC,g.title ASC,g.id ASC LIMIT ?"
	case "ADDED_DESC":
		order = " ORDER BY g.created_at_ms DESC,g.title ASC,g.id ASC LIMIT ?"
	case "UPDATED_DESC":
		order = " ORDER BY g.updated_at_ms DESC,g.title ASC,g.id ASC LIMIT ?"
	}
	query = queryWithConditions(query, conditions, order)
	arguments = append(arguments, limit+1)
	rows, err := server.database.QueryContext(request.Context(), query, arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items, err := scanGameListRows(rows, includeDeleted, limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := server.projectGameListAssociations(
		request.Context(), principal.ProfileID, items, includeDeleted,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items, nextCursor, err := server.encodeGameListNextCursor(items, limit, operationID, filterDigest, sortCode)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	response := map[string]any{
		"generatedAtMs": server.now().UnixMilli(), "items": items, "nextCursor": nextCursor,
	}
	if !includeDeleted && values.Get("cursor") == "" {
		filteredCount, facets, facetErr := queryGameListFacets(
			request.Context(), server.database, baseConditions, baseArguments,
		)
		if facetErr != nil {
			server.databaseError(writer, request, facetErr)
			return
		}
		response["filteredCount"] = filteredCount
		response["facets"] = facets
	}
	writeJSON(writer, http.StatusOK, response)
}

func gameListOperationID(includeDeleted bool) string {
	if includeDeleted {
		return "getAdminGames"
	}
	return "getGames"
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
