package home

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/home"
	"retrom/internal/repo/storequery"
)

type Repository struct{ database *sql.DB }

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) Summary(ctx context.Context, profileID string) (application.Summary, error) {
	var result application.Summary
	queries := []struct {
		query string
		args  []any
		dest  *int64
	}{
		{`
SELECT count(*)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.status='PUBLISHED' AND pi.enabled=1`, nil, &result.GameCount},
		{`
SELECT count(*)
FROM save_states s
JOIN (` + storequery.SaveRuntimeCompatibility + `) runtime_compatibility
 ON runtime_compatibility.save_state_id=s.id AND runtime_compatibility.status='AVAILABLE'
JOIN games g ON g.id=s.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE s.deleted_at_ms IS NULL AND s.profile_id=?
 AND g.status='PUBLISHED' AND pi.enabled=1`, []any{profileID}, &result.SaveCount},
		{`
SELECT count(*)
FROM import_items item
WHERE item.state='REVIEW_PENDING'
 AND (item.review_handoff_kind='DIRECT' OR EXISTS (
  SELECT 1 FROM emulationstation_import_items source
  WHERE source.library_import_item_id=item.id
   AND source.execution_state='REVIEW_PENDING'
 ))`, nil, &result.ReviewCount},
		{`
SELECT COALESCE(sum(ps.active_duration_ms),0)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE g.status='PUBLISHED' AND pi.enabled=1 AND ps.profile_id=?`, []any{profileID}, &result.ActiveDurationMS},
	}
	for _, item := range queries {
		if err := repository.database.QueryRowContext(ctx, item.query, item.args...).Scan(item.dest); err != nil {
			return application.Summary{}, fmt.Errorf("query home summary: %w", err)
		}
	}
	return result, nil
}

func (repository *Repository) RecentSaves(ctx context.Context, profileID string) ([]application.RecentSave, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT s.id,s.game_id,m.title,s.name,s.created_at_ms,native.last_synced_at_ms,s.active_duration_ms,
s.disc_index,s.screenshot_blob_id IS NOT NULL
FROM save_states s
LEFT JOIN game_save_versions native ON native.save_state_id=s.id
JOIN (`+storequery.SaveRuntimeCompatibility+`) runtime_compatibility
 ON runtime_compatibility.save_state_id=s.id AND runtime_compatibility.status='AVAILABLE'
JOIN games g ON g.id=s.game_id
JOIN games m ON m.id=g.id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE s.deleted_at_ms IS NULL AND s.profile_id=? AND g.status='PUBLISHED' AND pi.enabled=1
ORDER BY COALESCE(native.last_synced_at_ms,s.created_at_ms) DESC,s.id DESC LIMIT 3`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query recent saves: %w", err)
	}
	defer func() { cleanup.Error("close recent saves", rows.Close()) }()
	result := make([]application.RecentSave, 0, 3)
	for rows.Next() {
		var item application.RecentSave
		var synced, disc sql.NullInt64
		if err := rows.Scan(&item.SaveStateID, &item.GameID, &item.GameTitle, &item.Name,
			&item.CreatedAtMS, &synced, &item.ActiveDurationMS, &disc, &item.HasScreenshot); err != nil {
			return nil, fmt.Errorf("scan recent save: %w", err)
		}
		item.LastSyncedAtMS = int64Pointer(synced)
		item.DiscIndex = int64Pointer(disc)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent saves: %w", err)
	}
	return result, nil
}

func (repository *Repository) RecentGames(
	ctx context.Context, profileID string, includeDeleted bool,
) ([]application.RecentGame, error) {
	status := "g.status='PUBLISHED' AND pi.enabled=1"
	if includeDeleted {
		status = "g.status IN ('PUBLISHED','DELETED') AND (g.status='DELETED' OR pi.enabled=1)"
	}
	rows, err := repository.database.QueryContext(ctx, `
SELECT g.id,m.title,p.id,p.name,pi.id,pi.name,max(ps.started_at_ms),sum(ps.active_duration_ms),
count(ps.id),g.status,
(SELECT a.id FROM game_assets a
 WHERE a.game_id=g.id AND a.kind='COVER'
 ORDER BY a.ordinal,a.id LIMIT 1)
FROM play_sessions ps
JOIN games g ON g.id=ps.game_id
JOIN games m ON m.id=g.id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE `+status+` AND ps.profile_id=?
GROUP BY g.id,m.title,p.id,p.name,pi.id,pi.name,g.status
ORDER BY max(ps.started_at_ms) DESC,g.id DESC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query recent games: %w", err)
	}
	defer func() { cleanup.Error("close recent games", rows.Close()) }()
	result := make([]application.RecentGame, 0)
	for rows.Next() {
		var item application.RecentGame
		var cover sql.NullString
		if err := rows.Scan(&item.GameID, &item.Title, &item.Platform.ID, &item.Platform.Name,
			&item.PlatformInstance.ID, &item.PlatformInstance.Name, &item.LastPlayedAtMS,
			&item.ActiveDurationMS, &item.SessionCount, &item.Status, &cover); err != nil {
			return nil, fmt.Errorf("scan recent game: %w", err)
		}
		item.Availability = item.Status
		item.CoverAssetID = stringPointer(cover)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent games: %w", err)
	}
	return result, nil
}

func (repository *Repository) LatestGames(ctx context.Context) ([]application.LatestGame, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT g.id,m.title,p.id,p.name,pi.id,pi.name,g.created_at_ms,
(SELECT a.id FROM game_assets a WHERE a.game_id=g.id AND a.kind='COVER' ORDER BY a.ordinal,a.id LIMIT 1)
FROM games g JOIN games m ON m.id=g.id JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED' AND pi.enabled=1
ORDER BY g.created_at_ms DESC,g.id DESC LIMIT 10`)
	if err != nil {
		return nil, fmt.Errorf("query latest games: %w", err)
	}
	defer func() { cleanup.Error("close latest games", rows.Close()) }()
	result := make([]application.LatestGame, 0, 10)
	for rows.Next() {
		var item application.LatestGame
		var cover sql.NullString
		if err := rows.Scan(&item.GameID, &item.Title, &item.Platform.ID, &item.Platform.Name,
			&item.PlatformInstance.ID, &item.PlatformInstance.Name, &item.CreatedAtMS, &cover); err != nil {
			return nil, fmt.Errorf("scan latest game: %w", err)
		}
		item.CoverAssetID = stringPointer(cover)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest games: %w", err)
	}
	return result, nil
}

func (repository *Repository) FeaturedGame(
	ctx context.Context, profileID string,
) (application.FeaturedGame, bool, error) {
	var item application.FeaturedGame
	var cover sql.NullString
	err := repository.database.QueryRowContext(ctx, `
SELECT ps.launch_session_id,g.id,m.title,m.description,p.id,p.name,pi.id,pi.name,ps.started_at_ms,
(SELECT COALESCE(sum(all_sessions.active_duration_ms),0)
 FROM play_sessions all_sessions
 WHERE all_sessions.game_id=g.id AND all_sessions.profile_id=?),
(SELECT count(*) FROM play_sessions all_sessions WHERE all_sessions.game_id=g.id AND all_sessions.profile_id=?),
(SELECT a.id FROM game_assets a WHERE a.game_id=g.id AND a.kind='COVER' ORDER BY a.ordinal,a.id LIMIT 1)
FROM play_sessions ps JOIN games g ON g.id=ps.game_id JOIN games m ON m.id=g.id
JOIN platform_instances pi ON pi.id=g.platform_instance_id JOIN platforms p ON p.id=pi.platform_id
WHERE g.status='PUBLISHED' AND pi.enabled=1 AND ps.profile_id=?
ORDER BY ps.started_at_ms DESC,ps.id DESC LIMIT 1`, profileID, profileID, profileID).Scan(
		&item.LaunchID, &item.GameID, &item.Title, &item.Description, &item.Platform.ID,
		&item.Platform.Name, &item.PlatformInstance.ID, &item.PlatformInstance.Name,
		&item.LastPlayedAtMS, &item.ActiveDurationMS, &item.SessionCount, &cover)
	if errors.Is(err, sql.ErrNoRows) {
		return application.FeaturedGame{}, false, nil
	}
	if err != nil {
		return application.FeaturedGame{}, false, fmt.Errorf("query featured game: %w", err)
	}
	item.CoverAssetID = stringPointer(cover)
	if err := repository.database.QueryRowContext(
		ctx, `
SELECT count(*) FROM save_states save JOIN (`+storequery.SaveRuntimeCompatibility+`) compatibility
 ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
WHERE save.game_id=? AND save.profile_id=? AND save.deleted_at_ms IS NULL`, item.GameID, profileID,
	).Scan(&item.SaveCount); err != nil {
		return application.FeaturedGame{}, false, fmt.Errorf("query featured save count: %w", err)
	}
	var save application.FeaturedSave
	var disc sql.NullInt64
	err = repository.database.QueryRowContext(ctx, `
SELECT save.id,save.created_at_ms,save.active_duration_ms,save.disc_index,save.screenshot_blob_id IS NOT NULL
FROM save_states save LEFT JOIN game_save_versions native ON native.save_state_id=save.id
JOIN (`+storequery.SaveRuntimeCompatibility+`) compatibility
 ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
WHERE COALESCE(native.last_writer_launch_session_id,save.source_launch_session_id)=?
 AND save.profile_id=? AND save.deleted_at_ms IS NULL
ORDER BY save.created_at_ms DESC,save.id DESC LIMIT 1`, item.LaunchID, profileID).Scan(
		&save.SaveStateID, &save.CreatedAtMS, &save.ActiveDurationMS, &disc, &save.HasScreenshot)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		item.LastSessionSave = nil
	case err != nil:
		return application.FeaturedGame{}, false, fmt.Errorf("query featured session save: %w", err)
	default:
		save.DiscIndex = int64Pointer(disc)
		item.LastSessionSave = &save
	}
	return item, true, nil
}

func (repository *Repository) Platforms(ctx context.Context, profileID string) ([]application.Platform, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT p.id,p.name,count(DISTINCT g.id),count(ps.id)
FROM platforms p
LEFT JOIN platform_instances pi ON pi.platform_id=p.id AND pi.enabled=1 AND pi.deleted_at_ms IS NULL
LEFT JOIN games g ON g.platform_instance_id=pi.id AND g.status='PUBLISHED'
LEFT JOIN play_sessions ps ON ps.game_id=g.id AND ps.profile_id=?
WHERE EXISTS (SELECT 1 FROM platform_cores pc WHERE pc.platform_id=p.id AND pc.enabled=1)
GROUP BY p.id,p.name ORDER BY p.name COLLATE NOCASE,p.id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query home platforms: %w", err)
	}
	defer func() { cleanup.Error("close home platforms", rows.Close()) }()
	result := make([]application.Platform, 0, 8)
	for rows.Next() {
		var item application.Platform
		if err := rows.Scan(&item.ID, &item.Name, &item.GameCount, &item.PlayCount); err != nil {
			return nil, fmt.Errorf("scan home platform: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate home platforms: %w", err)
	}
	return result, nil
}

func int64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

var _ application.Repository = (*Repository)(nil)
