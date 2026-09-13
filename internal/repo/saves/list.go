package saves

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/storequery"
	application "retrom/internal/service/saves"
)

const saveListSQL = `
SELECT s.id,
s.game_id,
m.title,
s.name,
s.version,
s.created_at_ms,
native.last_synced_at_ms,
s.active_duration_ms,
s.payload_size_bytes,
source_launch.core_id,
c.name,
g.status,
p.id,
p.name,
pi.id,
pi.name,
s.disc_index,
s.screenshot_blob_id IS NOT NULL,
runtime_compatibility.status
FROM save_states s
LEFT JOIN game_save_versions native ON native.save_state_id=s.id
JOIN (` + storequery.SaveRuntimeCompatibility + `) runtime_compatibility
  ON runtime_compatibility.save_state_id=s.id
JOIN games g ON g.id=s.game_id
JOIN games m ON m.id=g.id
JOIN launch_sessions source_launch ON source_launch.id=s.source_launch_session_id
JOIN cores c ON c.id=source_launch.core_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
`

func (repository *Repository) List(
	ctx context.Context,
	query application.ListQuery,
) ([]application.ListItem, error) {
	conditions := []string{"s.profile_id=?", "s.deleted_at_ms IS NULL", "pi.enabled=1"}
	arguments := []any{query.ProfileID}
	if query.Query != "" {
		conditions = append(conditions, "(instr(g.search_text,?)>0 OR instr(lower(s.name),?)>0)")
		arguments = append(arguments, query.Query, query.Query)
	}
	for _, filter := range []struct {
		value, column string
	}{
		{query.GameID, "s.game_id"},
		{query.PlatformID, "pi.platform_id"},
		{query.PlatformInstanceID, "pi.id"},
		{query.CoreID, "source_launch.core_id"},
	} {
		if filter.value != "" {
			conditions = append(conditions, filter.column+"=?")
			arguments = append(arguments, filter.value)
		}
	}
	switch query.Availability {
	case "AVAILABLE":
		conditions = append(conditions, "g.status='PUBLISHED'", "runtime_compatibility.status='AVAILABLE'")
	case "BLOCKED":
		conditions = append(conditions, "(g.status!='PUBLISHED' OR runtime_compatibility.status!='AVAILABLE')")
	case "ALL":
	default:
		return nil, fmt.Errorf("read save list: %w", application.ErrInvalid)
	}
	if query.CursorCreatedAtMS != nil {
		conditions = append(conditions, `(COALESCE(native.last_synced_at_ms,s.created_at_ms)<?
 OR (COALESCE(native.last_synced_at_ms,s.created_at_ms)=? AND s.id<?))`)
		arguments = append(arguments, *query.CursorCreatedAtMS, *query.CursorCreatedAtMS, query.CursorID)
	}
	querySQL := saveListSQL + " WHERE " + strings.Join(conditions, " AND ") +
		" ORDER BY COALESCE(native.last_synced_at_ms,s.created_at_ms) DESC,s.id DESC LIMIT ?"
	arguments = append(arguments, query.Limit)
	rows, err := (records{executor: repository.database}).executor.QueryContext(ctx, querySQL, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query save list: %w", err)
	}
	defer func() { cleanup.Error("close save list", rows.Close()) }()
	items := make([]application.ListItem, 0, query.Limit)
	for rows.Next() {
		var item application.ListItem
		var discIndex, syncedAt sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.GameID, &item.GameTitle, &item.Name, &item.Version,
			&item.CreatedAtMS, &syncedAt, &item.ActiveDurationMS, &item.SizeBytes,
			&item.CoreID, &item.CoreName, &item.GameStatus, &item.PlatformID,
			&item.PlatformName, &item.InstanceID, &item.InstanceName, &discIndex,
			&item.HasScreenshot, &item.CompatibilityStatus,
		); err != nil {
			return nil, fmt.Errorf("scan save list: %w", err)
		}
		if discIndex.Valid {
			value := discIndex.Int64
			item.DiscIndex = &value
		}
		if syncedAt.Valid {
			value := syncedAt.Int64
			item.LastSyncedAtMS = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate save list: %w", err)
	}
	return items, nil
}

var _ application.ListRepository = (*Repository)(nil)
