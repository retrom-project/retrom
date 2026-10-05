package recordstore

import (
	"context"
	"database/sql"

	dbapi "retrom/internal/database"
)

func UpdateBiosRequirements(
	ctx context.Context, db dbapi.Executor, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"bios_requirements",
		"id,delivery_kind,emulator_path",
		BiosRequirementsUpdateRule,
	)
}

const BiosRequirementsUpdateRule = `
WITH previous(id,delivery_kind,emulator_path)
AS (VALUES(?::text,?::text,?::text))
SELECT CASE
-- bios_requirements_delivery_update
WHEN ((candidate.delivery_kind IS DISTINCT FROM previous.delivery_kind OR candidate.emulator_path IS DISTINCT
 FROM previous.emulator_path) AND (NOT (
  (candidate.delivery_kind='BIOS_BUNDLE' AND candidate.emulator_path IS NULL) OR
  (candidate.delivery_kind='EXTERNAL_FILE' AND
   candidate.emulator_path IS NOT NULL AND
   length(candidate.emulator_path) BETWEEN 1 AND 512 AND
   substr(candidate.emulator_path,1,1)='/' AND
   strpos(candidate.emulator_path,chr(92))=0 AND
   candidate.emulator_path NOT LIKE '%?%' AND
   candidate.emulator_path NOT LIKE '%#%' AND
   true AND
   candidate.emulator_path NOT LIKE '%//%' AND
   candidate.emulator_path NOT LIKE '%/./%' AND
   candidate.emulator_path NOT LIKE '%/../%' AND
   candidate.emulator_path NOT LIKE '%/.' AND
   candidate.emulator_path NOT LIKE '%/..')
))) THEN 'invalid BIOS delivery'
ELSE '' END
FROM bios_requirements candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
