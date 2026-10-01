package home

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/home"
)

const recentFrom = ` FROM profile_game_activity activity
JOIN games g ON g.id=activity.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id `

func recentConditions(query application.RecentQuery) ([]string, []any) {
	conditions := []string{"activity.profile_id=?", "g.status='PUBLISHED' AND pi.enabled=1"}
	args := []any{query.ProfileID}
	if query.IncludeDeleted {
		conditions[1] = "g.status IN ('PUBLISHED','DELETED') AND (g.status='DELETED' OR pi.enabled=1)"
	}
	if query.PlatformID != "" {
		conditions = append(conditions, "p.id=?")
		args = append(args, query.PlatformID)
	}
	if query.FromAtMS != nil {
		conditions = append(conditions, "activity.last_played_at_ms>=?")
		args = append(args, *query.FromAtMS)
	}
	if query.Query != "" {
		conditions = append(conditions, `(instr(g.search_text,?)>0 OR instr(lower(p.name),?)>0
OR instr(lower(pi.name),?)>0 OR EXISTS(SELECT 1 FROM game_tags relation
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
WHERE relation.game_id=g.id AND instr(tag.search_text,?)>0))`)
		args = append(args, query.Query, query.Query, query.Query, query.Query)
	}
	return conditions, args
}

func recentOrder(query application.RecentQuery) (string, string, []any) {
	columns := []string{"activity.last_played_at_ms", "activity.game_id"}
	direction, comparison := " DESC", "<"
	switch query.Sort {
	case application.RecentSortTitle:
		columns = []string{"g.title", "g.id"}
		direction, comparison = " ASC", ">"
	case application.RecentSortDuration:
		columns = append([]string{"activity.active_duration_ms"}, columns...)
	case application.RecentSortSessions:
		columns = append([]string{"activity.session_count"}, columns...)
	}
	order := strings.Join(columns, direction+",") + direction
	if query.Cursor == nil {
		return order, "", nil
	}
	args := make([]any, 0, len(columns))
	for _, value := range query.Cursor.Values {
		if query.Sort == application.RecentSortTitle {
			args = append(args, value)
			continue
		}
		parsed, _ := strconv.ParseInt(value, 10, 64)
		args = append(args, parsed)
	}
	args = append(args, query.Cursor.GameID)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")
	return order, "(" + strings.Join(columns, ",") + ")" + comparison + "(" + placeholders + ")", args
}

func (repository *Repository) RecentPage(
	ctx context.Context, query application.RecentQuery,
) ([]application.RecentGame, error) {
	conditions, args := recentConditions(query)
	order, boundary, cursorArgs := recentOrder(query)
	if boundary != "" {
		conditions = append(conditions, boundary)
		args = append(args, cursorArgs...)
	}
	args = append(args, query.Limit)
	rows, err := repository.database.QueryContext(ctx, `SELECT g.id,g.title,p.id,p.name,pi.id,pi.name,
activity.last_played_at_ms,activity.active_duration_ms,activity.session_count,g.status,
CASE WHEN g.status='PUBLISHED' THEN
(SELECT a.id FROM game_assets a WHERE a.game_id=g.id AND a.kind='COVER' ORDER BY a.ordinal,a.id LIMIT 1)
ELSE NULL END`+
		recentFrom+" WHERE "+strings.Join(conditions, " AND ")+" ORDER BY "+order+" LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("query recent page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]application.RecentGame, 0, query.Limit)
	for rows.Next() {
		var item application.RecentGame
		var cover sql.NullString
		if err := rows.Scan(&item.GameID, &item.Title, &item.Platform.ID, &item.Platform.Name,
			&item.PlatformInstance.ID, &item.PlatformInstance.Name, &item.LastPlayedAtMS,
			&item.ActiveDurationMS, &item.SessionCount, &item.Status, &cover); err != nil {
			return nil, fmt.Errorf("scan recent page: %w", err)
		}
		item.Availability, item.CoverAssetID = item.Status, stringPointer(cover)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent page: %w", err)
	}
	return items, nil
}

func (repository *Repository) RecentOverview(
	ctx context.Context, query application.RecentQuery,
) (application.RecentOverview, error) {
	result := application.RecentOverview{Platforms: []application.Platform{}}
	conditions, args := recentConditions(query)
	if err := dbapi.QueryRowContext(ctx, repository.database, "SELECT count(*)"+recentFrom+
		" WHERE "+strings.Join(conditions, " AND "), args...).Scan(&result.FilteredCount); err != nil {
		return result, fmt.Errorf("count recent games: %w", err)
	}
	query.Query, query.PlatformID, query.FromAtMS = "", "", nil
	conditions, args = recentConditions(query)
	where := " WHERE " + strings.Join(conditions, " AND ")
	if err := dbapi.QueryRowContext(ctx, repository.database,
		"SELECT count(*),COALESCE(sum(activity.active_duration_ms),0),COALESCE(sum(activity.session_count),0)"+
			recentFrom+where, args...).Scan(
		&result.Stats.GameCount, &result.Stats.ActiveDurationMS, &result.Stats.SessionCount); err != nil {
		return result, fmt.Errorf("read recent stats: %w", err)
	}
	rows, err := repository.database.QueryContext(ctx,
		"SELECT p.id,p.name,count(*),sum(activity.session_count)"+recentFrom+where+
			" GROUP BY p.id,p.name ORDER BY p.name,p.id", args...)
	if err != nil {
		return result, fmt.Errorf("read recent platforms: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item application.Platform
		if err := rows.Scan(&item.ID, &item.Name, &item.GameCount, &item.PlayCount); err != nil {
			return result, fmt.Errorf("scan recent platform: %w", err)
		}
		result.Platforms = append(result.Platforms, item)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate recent platforms: %w", err)
	}
	return result, nil
}
