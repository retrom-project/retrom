package recordstore

import (
	"context"
	"database/sql"
)

func CreateReviewEvents(
	ctx context.Context, db DBTX, query string, args ...any,
) (sql.Result, error) {
	return create(ctx, db, query, args, "id", ValidateReviewEvents)
}

func ValidateReviewEvents(ctx context.Context, db DBTX, keys ...any) error {
	return validate(ctx, db, review_eventsOwnership, keys)
}

const review_eventsOwnership = `
SELECT CASE
WHEN (EXISTS(
  SELECT 1
  FROM json_each(json_array(
    candidate.before_json,candidate.after_json,candidate.diff_json,candidate.config_evidence_json,
    candidate.dat_evidence_json,candidate.provider_evidence_json
  )) document
  JOIN json_tree(document.value) node
  WHERE lower(COALESCE(node.key,'')) IN (
    'assetid','candidateassetid','covercandidateassetid','coveruploadedassetid',
    'backgroundcandidateassetid','screenshotcandidateassetids','selectedassets',
    'blobid','archiveblobid','uploadid','uploadfileid','pegasusassetid',
    'url','coverurl','videourl','path','relativepath','sourcepath',
    'sha256','md5','hash','mime','mediatype','widthpx','heightpx',
    'sourcemanifest','sourcemanifestdigest','dependencysnapshot','configsnapshot'
  )
)) THEN 'review event v2 contains payload evidence'
ELSE '' END
FROM review_events candidate
WHERE candidate.id=?`
