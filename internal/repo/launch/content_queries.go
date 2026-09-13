package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/launch"
)

type ContentQueries struct{ executor dbexec.Executor }

func NewContentQueries(executor dbexec.Executor) *ContentQueries {
	return &ContentQueries{executor: executor}
}

func (repository *ContentQueries) ProductContent(
	ctx context.Context,
	id, logicalName string,
	folded bool,
) (application.ContentRecord, bool, error) {
	foldedFlag := 0
	if folded {
		foldedFlag = 1
	}
	return scanContent(repository.executor.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.hard_expires_at_ms,
b.sha256,
lc.format_version,
l.core_id,
l.provider_id,l.target_id,l.bundle_sha256,
COALESCE(platform.id,'rpgmaker'),
l.dos_entry_path,
(SELECT count(*) FROM launch_external_files file
 WHERE file.launch_session_id=l.id AND file.kind='DISC')
FROM launch_sessions l
JOIN launch_content_files lc ON lc.launch_session_id=l.id
JOIN blobs b ON b.id=lc.blob_id
LEFT JOIN games game ON game.id=l.game_id
LEFT JOIN platform_instances instance ON instance.id=game.platform_instance_id
LEFT JOIN platforms platform ON platform.id=instance.platform_id
WHERE l.id=?
AND (
 (?=0 AND lc.logical_name=?)
 OR (
  ?=1
  AND lc.format_version='RPG_MAKER_PROJECT'
  AND lc.logical_name=? COLLATE NOCASE
  AND 1=(
   SELECT count(*)
   FROM launch_content_files candidate
   WHERE candidate.launch_session_id=l.id
   AND candidate.format_version='RPG_MAKER_PROJECT'
   AND candidate.logical_name=? COLLATE NOCASE
  )
 )
)

`, id, foldedFlag, logicalName, foldedFlag, logicalName, logicalName))
}

func (repository *ContentQueries) PreviewContent(
	ctx context.Context,
	id, logicalName string,
) (application.ContentRecord, bool, error) {
	return scanContent(repository.executor.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,blob.sha256,
preview.content_format,binding.core_id,preview.provider_id,preview.target_id,
preview.bundle_sha256,platform.id,preview.default_dos_entry,
(SELECT count(*) FROM review_preview_files file WHERE file.preview_session_id=preview.id AND file.role='DISC')
FROM review_preview_sessions preview
JOIN blobs blob ON blob.id=preview.content_blob_id
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND binding.target_id=preview.target_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE preview.id=? AND preview.content_logical_name=?
`, id, logicalName))
}

func (repository *ContentQueries) PreviewProject(
	ctx context.Context,
	id, logicalName string,
	folded bool,
) (application.ContentRecord, bool, error) {
	return scanContent(repository.executor.QueryRowContext(ctx, `
WITH preview_files AS (
 SELECT id AS preview_session_id,content_logical_name AS logical_name,content_blob_id AS blob_id
 FROM review_preview_sessions WHERE id=?
 UNION ALL
 SELECT preview_session_id,logical_name,blob_id FROM review_preview_files
 WHERE preview_session_id=? AND role IN ('PROJECT_FILE','RUNTIME_FILE')
)
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,blob.sha256,
preview.content_format,binding.core_id,preview.provider_id,preview.target_id,
preview.bundle_sha256,platform.id,NULL,0
FROM review_preview_sessions preview
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND binding.target_id=preview.target_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN preview_files file ON file.preview_session_id=preview.id AND (
 file.logical_name=? OR ? AND preview.content_format='RPG_MAKER_PROJECT' AND lower(file.logical_name)=lower(?)
 AND NOT EXISTS(SELECT 1 FROM preview_files exact WHERE exact.logical_name=?)
 AND (SELECT count(*) FROM preview_files folded WHERE lower(folded.logical_name)=lower(?))=1
)
JOIN blobs blob ON blob.id=file.blob_id
WHERE preview.id=?

`, id, id, logicalName, folded, logicalName, logicalName, logicalName, id))
}

func scanContent(row dbexec.Scanner) (application.ContentRecord, bool, error) {
	var result application.ContentRecord
	var dosEntry sql.NullString
	session, content := &result.Session, &result.Content
	err := row.Scan(&session.CredentialHash, &session.State, &session.HardExpiresAtMS,
		&content.Digest, &content.Format, &content.CoreID, &content.ProviderID, &content.TargetID,
		&content.BundleSHA256, &content.PlatformKey, &dosEntry, &content.DiscCount)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ContentRecord{}, false, nil
	}
	if err != nil {
		return application.ContentRecord{}, false, fmt.Errorf("scan launch content: %w", err)
	}
	if dosEntry.Valid {
		content.DOSEntry = &dosEntry.String
	}
	return result, true, nil
}
