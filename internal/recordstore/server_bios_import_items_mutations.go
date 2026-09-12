package recordstore

import (
	"context"
	"database/sql"
)

func UpdateServerBiosImportItems(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"server_bios_import_items",
		"server_import_id,requirement_id,new_installation_id,previous_installation_id",
		ServerBiosImportItemsUpdateRule,
	)
}

const ServerBiosImportItemsUpdateRule = `
WITH previous(server_import_id,requirement_id,new_installation_id,previous_installation_id) AS (VALUES(?,
?,?,?))
SELECT CASE
-- server_bios_items_installation_update
WHEN ((candidate.previous_installation_id IS NOT previous.previous_installation_id OR
candidate.new_installation_id IS NOT previous.new_installation_id) AND
((candidate.previous_installation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bios_installations installation
  WHERE installation.id=candidate.previous_installation_id AND
installation.requirement_id=candidate.requirement_id
)) OR (candidate.new_installation_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM bios_installations installation
  WHERE installation.id=candidate.new_installation_id AND
installation.requirement_id=candidate.requirement_id
)))) THEN 'server BIOS item installation owner mismatch'
ELSE '' END
FROM server_bios_import_items candidate CROSS JOIN previous
WHERE candidate.server_import_id=previous.server_import_id AND
candidate.requirement_id=previous.requirement_id`
