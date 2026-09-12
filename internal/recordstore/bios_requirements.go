package recordstore

import (
	"context"
	"database/sql"
)

func CreateBiosRequirements(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateBiosRequirements)
}

func ValidateBiosRequirements(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, bios_requirementsOwnership, keys)
}

const bios_requirementsOwnership = `
SELECT CASE
WHEN (NOT (
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
)) THEN 'invalid BIOS delivery'
WHEN (NOT EXISTS(
  SELECT 1 FROM runtime_targets target
  WHERE target.provider_id=candidate.provider_id AND target.target_id=candidate.target_id
)) THEN 'invalid runtime target snapshot'
ELSE '' END
FROM bios_requirements candidate
WHERE candidate.id=?`
