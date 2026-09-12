package recordstore

import (
	"context"
	"database/sql"
)

func UpdateBiosInstallations(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"bios_installations",
		"id,blob_id,payload_released_at_ms,requirement_id,server_import_candidate_id,"+
			"source_kind",
		BiosInstallationsUpdateRule,
	)
}

const BiosInstallationsUpdateRule = `
WITH previous(id,blob_id,payload_released_at_ms,requirement_id,server_import_candidate_id,source_kind)
AS (VALUES(?,?,?,?,?,?))
SELECT CASE
-- bios_installations_payload_terminal
WHEN ((candidate.blob_id IS NOT previous.blob_id OR candidate.payload_released_at_ms IS NOT
previous.payload_released_at_ms) AND (previous.blob_id IS NULL AND candidate.blob_id IS NOT NULL)) THEN
'released BIOS payload is terminal'
-- bios_installations_source_update
WHEN ((candidate.source_kind IS NOT previous.source_kind OR candidate.server_import_candidate_id IS NOT
previous.server_import_candidate_id OR candidate.requirement_id IS NOT previous.requirement_id) AND
(candidate.source_kind<>previous.source_kind OR candidate.server_import_candidate_id IS NOT
previous.server_import_candidate_id OR candidate.requirement_id<>previous.requirement_id)) THEN
'immutable BIOS installation source'
ELSE '' END
FROM bios_installations candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
