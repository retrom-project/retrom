package immersive

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/model/immersive"
	"retrom/internal/repo/storequery"
)

func (records saveRecords) ForGames(
	ctx context.Context,
	profileID string,
	gameIDs []string,
) (map[string][]immersive.SaveState, error) {
	result := make(map[string][]immersive.SaveState, len(gameIDs))
	if len(gameIDs) == 0 {
		return result, nil
	}
	arguments := make([]any, 0, len(gameIDs)+1)
	arguments = append(arguments, profileID)
	for _, id := range gameIDs {
		arguments = append(arguments, id)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(gameIDs)), ",")
	rows, err := records.database.QueryContext(ctx, `
SELECT save.game_id,save.id,save.name,save.created_at_ms,native.last_synced_at_ms,
       save.payload_size_bytes,save.disc_index,
       save.screenshot_blob_id IS NOT NULL
FROM save_states save
LEFT JOIN game_save_versions native ON native.save_state_id=save.id
JOIN (`+storequery.SaveRuntimeCompatibility+`) compatibility
  ON compatibility.save_state_id=save.id AND compatibility.status='AVAILABLE'
WHERE save.profile_id=? AND save.deleted_at_ms IS NULL
AND save.game_id IN (`+placeholders+`)
ORDER BY save.game_id,COALESCE(native.last_synced_at_ms,save.created_at_ms) DESC,save.id DESC
`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("immersive: query save states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var gameID string
		var save immersive.SaveState
		var synced sql.NullInt64
		var discIndex sql.NullInt64
		if err := rows.Scan(
			&gameID,
			&save.ID,
			&save.Name,
			&save.CreatedAtMS,
			&synced,
			&save.SizeBytes,
			&discIndex,
			&save.HasScreenshot,
		); err != nil {
			return nil, fmt.Errorf("immersive: scan save state: %w", err)
		}
		save.DiscIndex = nullableInt64Pointer(discIndex)
		save.LastSyncedAtMS = nullableInt64Pointer(synced)
		result[gameID] = append(result[gameID], save)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("immersive: iterate save states: %w", err)
	}
	return result, nil
}
