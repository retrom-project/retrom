package recordstore

import (
	"context"
	"database/sql"
)

func UpdateBiosRequirements(
	ctx context.Context, db DBTX, change Update,
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
WITH previous(id,delivery_kind,emulator_path) AS (VALUES(?,?,?))
SELECT CASE
-- bios_requirements_delivery_update
WHEN ((candidate.delivery_kind IS NOT previous.delivery_kind OR candidate.emulator_path IS NOT
previous.emulator_path) AND (NOT (
  (candidate.delivery_kind='BIOS_BUNDLE' AND candidate.emulator_path IS NULL) OR
  (candidate.delivery_kind='EXTERNAL_FILE' AND
   candidate.emulator_path IS NOT NULL AND
   length(candidate.emulator_path) BETWEEN 1 AND 512 AND
   substr(candidate.emulator_path,1,1)='/' AND
   candidate.emulator_path NOT LIKE '%\%' AND
   candidate.emulator_path NOT LIKE '%?%' AND
   candidate.emulator_path NOT LIKE '%#%' AND
   instr(candidate.emulator_path,char(0))=0 AND
   candidate.emulator_path NOT LIKE '%//%' AND
   candidate.emulator_path NOT LIKE '%/./%' AND
   candidate.emulator_path NOT LIKE '%/../%' AND
   candidate.emulator_path NOT LIKE '%/.' AND
   candidate.emulator_path NOT LIKE '%/..')
))) THEN 'invalid BIOS delivery'
ELSE '' END
FROM bios_requirements candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
