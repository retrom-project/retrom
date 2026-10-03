package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records *ContentDuplicates) PublishedMatches(
	ctx context.Context,
	query libraryservice.DuplicateQuery,
) ([]libraryservice.DuplicateGame, error) {
	statement := unorderedDuplicateQuery
	if query.ContentKind == "MULTI_DISC" {
		statement = orderedDuplicateQuery
	}
	rows, err := records.executor.QueryContext(ctx, statement, query.PlatformID, query.SnapshotID)
	if err != nil {
		return nil, fmt.Errorf("query published duplicates: %w", err)
	}
	defer func() { cleanup.Error("close published duplicates", rows.Close()) }()
	result := make([]libraryservice.DuplicateGame, 0)
	for rows.Next() {
		var game libraryservice.DuplicateGame
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

// Materializing the platform candidates prevents SQLite from running correlated
// content comparisons before the platform predicate. The incoming multiset is
// also computed once, not once for every published game.
const duplicateCandidates = `
WITH candidates AS MATERIALIZED (
 SELECT game.id,game.title,game.created_at_ms,game.content_kind,
  instance.id AS instance_id,instance.name AS instance_name
 FROM platform_instances instance JOIN games game ON game.platform_instance_id=instance.id
 WHERE instance.platform_id=? AND game.status='PUBLISHED'
), incoming AS MATERIALIZED (`

const unorderedDuplicateQuery = duplicateCandidates + `
 SELECT role,json_extract(file_record,'$.sha256') AS sha256,count(*) AS file_count
 FROM import_item_source_snapshot_files WHERE source_snapshot_id=?
 GROUP BY role,json_extract(file_record,'$.sha256')
)
SELECT game.id,game.title,game.instance_id,game.instance_name FROM candidates game
WHERE (SELECT count(*) FROM game_files WHERE game_id=game.id)=(SELECT coalesce(sum(file_count),0) FROM incoming)
AND NOT EXISTS(
 SELECT role,json_extract(file_record,'$.sha256'),count(*) FROM game_files WHERE game_id=game.id
 GROUP BY role,json_extract(file_record,'$.sha256')
 EXCEPT SELECT role,sha256,file_count FROM incoming
)
AND NOT EXISTS(
 SELECT role,sha256,file_count FROM incoming
 EXCEPT
 SELECT role,json_extract(file_record,'$.sha256'),count(*) FROM game_files WHERE game_id=game.id
 GROUP BY role,json_extract(file_record,'$.sha256')
)
ORDER BY game.created_at_ms,game.id`

const orderedDuplicateQuery = duplicateCandidates + `
 SELECT sort_order,json_extract(file_record,'$.sha256') AS sha256
 FROM import_item_source_snapshot_files WHERE source_snapshot_id=? AND role='DISC'
)
SELECT game.id,game.title,game.instance_id,game.instance_name FROM candidates game
WHERE game.content_kind='MULTI_DISC'
AND (SELECT count(*) FROM game_files WHERE game_id=game.id AND role='DISC')=(SELECT count(*) FROM incoming)
AND NOT EXISTS(
 SELECT sort_order,sha256 FROM incoming
 EXCEPT
 SELECT sort_order,json_extract(file_record,'$.sha256') FROM game_files WHERE game_id=game.id AND role='DISC'
)
AND NOT EXISTS(
 SELECT sort_order,json_extract(file_record,'$.sha256') FROM game_files WHERE game_id=game.id AND role='DISC'
 EXCEPT SELECT sort_order,sha256 FROM incoming
)
ORDER BY game.created_at_ms,game.id`
