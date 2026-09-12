package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewDraftRuntimePackSelections(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "review_draft_id,slot", ValidateReviewDraftRuntimePackSelections)
}

func ValidateReviewDraftRuntimePackSelections(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_draft_runtime_pack_selectionsOwnership, keys)
}

const review_draft_runtime_pack_selectionsOwnership = `
SELECT CASE
WHEN (NOT EXISTS(
  SELECT 1 FROM rpgmaker_review_profiles profile
  JOIN review_drafts draft ON draft.id=profile.review_draft_id
  JOIN import_items item ON item.id=draft.import_item_id
  JOIN runtime_asset_pack_definitions definition ON definition.id=candidate.definition_id
  JOIN runtime_asset_pack_installations installation
    ON installation.id=candidate.installation_id AND installation.definition_id=definition.id
  WHERE profile.review_draft_id=candidate.review_draft_id AND item.state='REVIEW_PENDING'
    AND definition.enabled=1 AND definition.generation=profile.generation
    AND definition.declared_name=candidate.declared_name
    AND definition.normalized_declared_name=candidate.normalized_declared_name
    AND installation.status='READY'
    AND installation.file_count=(SELECT count(*) FROM runtime_asset_pack_files file
      WHERE file.installation_id=installation.id)
    AND installation.total_bytes=(SELECT COALESCE(sum(file.size_bytes),0) FROM runtime_asset_pack_files
file
      WHERE file.installation_id=installation.id)
    AND (
      profile.generation IN ('RPG2000','RPG2003') AND candidate.slot=0
      OR profile.generation IN ('RPGXP','RPGVX','RPGVXACE') AND candidate.slot BETWEEN 1 AND 3
    )
)) THEN 'invalid review runtime pack selection'
ELSE '' END
FROM review_draft_runtime_pack_selections candidate
WHERE candidate.review_draft_id=? AND candidate.slot=?`
