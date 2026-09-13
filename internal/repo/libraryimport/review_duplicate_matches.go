package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/service/libraryimport"
)

func (records *ContentDuplicates) PublishedMatches(
	ctx context.Context,
	query application.DuplicateQuery,
) ([]application.DuplicateGame, error) {
	statement := unorderedDuplicateQuery
	if query.ContentKind == "MULTI_DISC" {
		statement = orderedDuplicateQuery
	}
	rows, err := records.executor.QueryContext(ctx, statement, query.PlatformID, query.SnapshotID, query.SnapshotID)
	if err != nil {
		return nil, fmt.Errorf("query published duplicates: %w", err)
	}
	defer func() { cleanup.Error("close published duplicates", rows.Close()) }()
	result := make([]application.DuplicateGame, 0)
	for rows.Next() {
		var game application.DuplicateGame
		if err := rows.Scan(&game.GameID, &game.Title, &game.PlatformInstanceID, &game.PlatformInstanceName); err != nil {
			return nil, fmt.Errorf("scan published duplicate: %w", err)
		}
		result = append(result, game)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published duplicates: %w", err)
	}
	return result, nil
}

const unorderedDuplicateQuery = `
SELECT game.id,game.title,instance.id,instance.name FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE game.status='PUBLISHED' AND instance.platform_id=?
AND NOT EXISTS(
 SELECT 1 FROM (
  SELECT existing.role,existing.blob_id,count(*) AS file_count FROM game_files existing
  WHERE existing.game_id=game.id GROUP BY existing.role,existing.blob_id
  EXCEPT
  SELECT incoming.role,incoming.blob_id,count(*) AS file_count FROM import_item_source_snapshot_files incoming
  WHERE incoming.source_snapshot_id=? GROUP BY incoming.role,incoming.blob_id
 ) existing_difference
)
AND NOT EXISTS(
 SELECT 1 FROM (
  SELECT incoming.role,incoming.blob_id,count(*) AS file_count FROM import_item_source_snapshot_files incoming
  WHERE incoming.source_snapshot_id=? GROUP BY incoming.role,incoming.blob_id
  EXCEPT
  SELECT existing.role,existing.blob_id,count(*) AS file_count FROM game_files existing
  WHERE existing.game_id=game.id GROUP BY existing.role,existing.blob_id
 ) incoming_difference
)
ORDER BY game.created_at_ms,game.id`

const orderedDuplicateQuery = `
SELECT game.id,game.title,instance.id,instance.name FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
WHERE game.status='PUBLISHED' AND instance.platform_id=? AND game.content_kind='MULTI_DISC'
AND NOT EXISTS(
 SELECT incoming.sort_order,incoming.blob_id FROM import_item_source_snapshot_files incoming
 WHERE incoming.source_snapshot_id=? AND incoming.role='DISC'
 EXCEPT
 SELECT existing.sort_order,existing.blob_id FROM game_files existing
 WHERE existing.game_id=game.id AND existing.role='DISC'
)
AND NOT EXISTS(
 SELECT existing.sort_order,existing.blob_id FROM game_files existing
 WHERE existing.game_id=game.id AND existing.role='DISC'
 EXCEPT
 SELECT incoming.sort_order,incoming.blob_id FROM import_item_source_snapshot_files incoming
 WHERE incoming.source_snapshot_id=? AND incoming.role='DISC'
)
ORDER BY game.created_at_ms,game.id`
