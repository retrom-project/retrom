package launch

import (
	"context"

	"retrom/internal/tyranoscript/detector"
)

const maximumTyranoScriptProjectFiles = 10_000

func (service *Service) reviewPreviewTyranoScriptContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	profile, err := detector.ParseSnapshot(source.DependencySnapshot)
	if err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return service.reviewPreviewProjectContent(
		ctx, source, profile.EntryPath, tyranoScriptProjectFormat, maximumTyranoScriptProjectFiles,
		"TyranoScript",
	)
}

func (service *Service) TyranoScriptProjectContentAuthorized(
	ctx context.Context,
	sessionID, logicalName string,
	preview bool,
) (ContentView, error) {
	if !preview {
		content, err := service.ContentAuthorized(ctx, sessionID, logicalName)
		if err != nil || content.Format != tyranoScriptProjectFormat {
			return ContentView{}, ErrCredential
		}
		return content, nil
	}
	var digest, format, providerID, targetID, targetContractSHA256 string
	err := service.database.QueryRowContext(ctx, `
SELECT blob.sha256,preview.content_format,preview.provider_id,preview.target_id,preview.target_contract_sha256
FROM review_preview_sessions preview
JOIN blobs blob ON blob.id=preview.content_blob_id
WHERE preview.id=? AND preview.state='ACTIVE' AND preview.hard_expires_at_ms>?
 AND preview.content_format='TYRANOSCRIPT_PROJECT' AND preview.content_logical_name=?
UNION ALL
SELECT blob.sha256,preview.content_format,preview.provider_id,preview.target_id,preview.target_contract_sha256
FROM review_preview_sessions preview
JOIN review_preview_files file ON file.preview_session_id=preview.id
JOIN blobs blob ON blob.id=file.blob_id
WHERE preview.id=? AND preview.state='ACTIVE' AND preview.hard_expires_at_ms>?
 AND preview.content_format='TYRANOSCRIPT_PROJECT' AND file.role='PROJECT_FILE' AND file.logical_name=?
`, sessionID, service.now().UnixMilli(), logicalName,
		sessionID, service.now().UnixMilli(), logicalName).Scan(
		&digest, &format, &providerID, &targetID, &targetContractSHA256,
	)
	if err != nil || format != tyranoScriptProjectFormat {
		return ContentView{}, ErrCredential
	}
	return ContentView{
		Digest: digest, Format: format, CoreID: "tyranoscript", PlatformKey: "tyranoscript",
		ProviderID: providerID, TargetID: targetID, TargetContractSHA256: targetContractSHA256,
	}, nil
}
