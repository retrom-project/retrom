package gamelist

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/gamelist"
)

func (repository *Repository) List(
	ctx context.Context, request application.ListRequest,
) (application.ListResult, error) {
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
	conditions, arguments := listConditions(request)
	baseConditions := append([]string(nil), conditions...)
	baseArguments := append([]any(nil), arguments...)
	query, conditions, arguments = appendCursor(query, request, conditions, arguments)
	order := listOrder(request.Sort)
	query = withConditions(query, conditions, order)
	arguments = append([]any{request.ProfileID}, arguments...)
	arguments = append(arguments, request.Limit)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return application.ListResult{}, fmt.Errorf("query game list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := application.ListResult{Items: make([]application.GameItem, 0, request.Limit)}
	for rows.Next() {
		item, err := scanGameItem(rows, request.IncludeDeleted)
		if err != nil {
			return application.ListResult{}, err
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return application.ListResult{}, fmt.Errorf("iterate game list: %w", err)
	}
	if request.IncludeFacets {
		result.FilteredCount, result.Facets, err = repository.facets(
			ctx, baseConditions, baseArguments,
		)
		if err != nil {
			return application.ListResult{}, err
		}
	}
	return result, nil
}

func listConditions(request application.ListRequest) ([]string, []any) {
	conditions := make([]string, 0, 5)
	arguments := make([]any, 0, 4)
	if !request.IncludeDeleted {
		conditions = append(conditions, "pi.enabled=1")
	}
	switch {
	case !request.IncludeDeleted || request.Filters.Status == "PUBLISHED":
		conditions = append(conditions, "g.status='PUBLISHED'")
	case request.Filters.Status == "DELETED":
		conditions = append(conditions, "g.status='DELETED'")
	}
	if request.Filters.Query != "" {
		conditions = append(conditions, `(instr(g.search_text,?)>0 OR EXISTS(
SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id=g.id AND instr(tag.search_text,?)>0))`)
		arguments = append(arguments, request.Filters.Query, request.Filters.Query)
	}
	if request.Filters.TagID != "" {
		conditions = append(conditions, `EXISTS(
SELECT 1 FROM game_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id=g.id AND tag.id=?)`)
		arguments = append(arguments, request.Filters.TagID)
	}
	if request.Filters.PlatformID != "" {
		conditions = append(conditions, "p.id=?")
		arguments = append(arguments, request.Filters.PlatformID)
	}
	if request.Filters.PlatformInstanceID != "" {
		conditions = append(conditions, "pi.id=?")
		arguments = append(arguments, request.Filters.PlatformInstanceID)
	}
	return conditions, arguments
}

func appendCursor(
	query string,
	request application.ListRequest,
	conditions []string,
	arguments []any,
) (string, []string, []any) {
	if request.Cursor == nil {
		return query, conditions, arguments
	}
	payload := request.Cursor
	switch request.Sort {
	case application.SortTitleAsc:
		if len(payload.SortValues) != 1 {
			return query, append(conditions, "0=1"), arguments
		}
		conditions = append(conditions, "(g.title>? OR (g.title=? AND g.id>?))")
		arguments = append(arguments, payload.SortValues[0], payload.SortValues[0], payload.ID)
	case application.SortAddedDesc, application.SortUpdatedDesc:
		if len(payload.SortValues) != 2 {
			return query, append(conditions, "0=1"), arguments
		}
		timestamp, err := parseCursorInt(payload.SortValues[0])
		if err != nil {
			return query, append(conditions, "0=1"), arguments
		}
		column := "g.created_at_ms"
		if request.Sort == application.SortUpdatedDesc {
			column = "g.updated_at_ms"
		}
		conditions = append(conditions, fmt.Sprintf(
			"(%s<? OR (%s=? AND (g.title>? OR (g.title=? AND g.id>?))))",
			column, column,
		))
		arguments = append(arguments, timestamp, timestamp, payload.SortValues[1], payload.SortValues[1], payload.ID)
	case application.SortRecentDesc:
		if len(payload.SortValues) != 3 {
			return query, append(conditions, "0=1"), arguments
		}
		lastPlayed, lastPlayedErr := parseCursorInt(payload.SortValues[0])
		createdAt, createdAtErr := parseCursorInt(payload.SortValues[1])
		if lastPlayedErr != nil || createdAtErr != nil {
			return query, append(conditions, "0=1"), arguments
		}
		lastPlayedExpression := `COALESCE((SELECT max(ps_cursor.started_at_ms)
FROM play_sessions ps_cursor WHERE ps_cursor.game_id=g.id AND ps_cursor.profile_id=?),-1)`
		conditions = append(conditions, fmt.Sprintf(
			`(%s<? OR (%s=? AND (g.created_at_ms<? OR (g.created_at_ms=? AND (g.title>? OR (g.title=? AND g.id>?))))))`,
			lastPlayedExpression, lastPlayedExpression,
		))
		arguments = append(arguments,
			request.ProfileID, lastPlayed, request.ProfileID, lastPlayed,
			createdAt, createdAt, payload.SortValues[2], payload.SortValues[2], payload.ID,
		)
	}
	return query, conditions, arguments
}

func listOrder(sort string) string {
	switch sort {
	case application.SortRecentDesc:
		return " ORDER BY last_played_at_ms DESC,g.created_at_ms DESC,g.title ASC,g.id ASC LIMIT ?"
	case application.SortAddedDesc:
		return " ORDER BY g.created_at_ms DESC,g.title ASC,g.id ASC LIMIT ?"
	case application.SortUpdatedDesc:
		return " ORDER BY g.updated_at_ms DESC,g.title ASC,g.id ASC LIMIT ?"
	default:
		return " ORDER BY g.title ASC,g.id ASC LIMIT ?"
	}
}

type rowScanner interface {
	Scan(...any) error
}

func scanGameItem(scanner rowScanner, includeAdminProjection bool) (application.GameItem, error) {
	var item application.GameItem
	var platformID, platformName, instanceID, instanceName, defaultCoreID, defaultCoreName string
	var lastPlayedAtMS, releaseYear sql.NullInt64
	var metadataComplete int64
	var runtimeStatus, coverAssetID sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.Title,
		&platformID,
		&platformName,
		&instanceID,
		&instanceName,
		&defaultCoreID,
		&defaultCoreName,
		&item.Status,
		&item.Version,
		&item.CreatedAtMS,
		&item.UpdatedAtMS,
		&lastPlayedAtMS,
		&releaseYear,
		&metadataComplete,
		&runtimeStatus,
		&coverAssetID,
	); err != nil {
		return application.GameItem{}, fmt.Errorf("scan game list item: %w", err)
	}
	item.Platform = application.NamedResource{ID: platformID, Name: platformName}
	item.PlatformInstance = application.NamedResource{ID: instanceID, Name: instanceName}
	item.DefaultCore = application.NamedResource{ID: defaultCoreID, Name: defaultCoreName}
	item.LastPlayedAtMS = nullableInt64Pointer(lastPlayedAtMS)
	item.CoverAssetID = nullableStringPointer(coverAssetID)
	if includeAdminProjection {
		item.ReleaseYear = nullableInt64Pointer(releaseYear)
		item.MetadataComplete = metadataComplete == 1
		item.RuntimeStatus = nullableStringPointer(runtimeStatus)
	}
	return item, nil
}
