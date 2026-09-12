package recordstore

import (
	"context"
	"database/sql"
)

func UpdateContentHashEvidence(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"content_hash_evidence",
		"id,crc32,created_at_ms,md5,payload_released_at_ms,profile,query_order,scrape_run_id,"+
			"sha1,sha256",
		ContentHashEvidenceUpdateRule,
	)
}

const ContentHashEvidenceUpdateRule = `
WITH previous(id,crc32,created_at_ms,md5,payload_released_at_ms,profile,query_order,scrape_run_id,sha1,
sha256) AS (VALUES(?,?,?,?,?,?,?,?,?,?))
SELECT CASE
-- content_hash_evidence_immutable_update
WHEN (NOT (
  previous.payload_released_at_ms IS NULL AND candidate.payload_released_at_ms IS NOT NULL
  AND candidate.blob_id IS NULL AND candidate.archive_blob_id IS NULL AND
candidate.archive_entry_ordinal IS NULL
  AND candidate.id=previous.id AND candidate.scrape_run_id=previous.scrape_run_id AND
candidate.profile=previous.profile
  AND candidate.crc32 IS previous.crc32 AND candidate.md5 IS previous.md5 AND candidate.sha1 IS
previous.sha1 AND candidate.sha256 IS previous.sha256
  AND candidate.query_order=previous.query_order AND candidate.created_at_ms=previous.created_at_ms
  AND EXISTS(
    SELECT 1 FROM metadata_scrape_runs run
    LEFT JOIN import_items item ON item.id=run.import_item_id
    LEFT JOIN games game ON game.id=run.game_id
    WHERE run.id=previous.scrape_run_id
      AND (item.payload_state IN ('RELEASING','FAILED') OR game.payload_state IN ('RELEASING','FAILED'))
  )
)) THEN 'immutable'
ELSE '' END
FROM content_hash_evidence candidate CROSS JOIN previous
WHERE candidate.id=previous.id`

func DeleteContentHashEvidence(
	ctx context.Context, db DBTX, scope Scope,
) (sql.Result, error) {
	return deleteRecords(
		ctx,
		db,
		scope,
		"content_hash_evidence",
		"id",
		ContentHashEvidenceDeleteRule,
	)
}

const ContentHashEvidenceDeleteRule = `
WITH previous(id) AS (VALUES(?))
SELECT CASE
-- content_hash_evidence_immutable_delete
WHEN (1=1) THEN 'immutable'
ELSE '' END
FROM previous`
