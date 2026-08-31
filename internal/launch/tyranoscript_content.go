package launch

import (
	"context"
	"fmt"

	"retrom/internal/tyranoscript/detector"
)

const maximumTyranoScriptProjectFiles = 10_000

func (service *Service) buildTyranoScriptProductContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	return service.buildRuntimeProjectProductContentPlan(ctx, selection, runtimeProjectContentDefinition{
		contentKind: tyranoScriptProjectFormat, runtimeFamily: "TYRANOSCRIPT",
		adapterKind: "TYRANOSCRIPT_WEB", adapterID: "tyranoscript-web",
		maximumFiles: maximumTyranoScriptProjectFiles,
		markerPath: func(raw string) (string, error) {
			profile, err := detector.ParseSnapshot(raw)
			if err != nil {
				return "", fmt.Errorf("parse TyranoScript project profile: %w", err)
			}
			return profile.EntryPath, nil
		},
		runtimeFilesValid: func(raw, runtimeVersion string) bool {
			compatibility, err := parseTyranoScriptCompatibility(raw)
			return err == nil && service.tyranoScriptBridgeAvailable(runtimeVersion, compatibility.BridgePath)
		},
	})
}

func (service *Service) validateTyranoScriptReviewPreviewSource(source reviewPreviewSource) error {
	if _, err := detector.ParseSnapshot(source.DependencySnapshot); err != nil {
		return ErrReviewPreviewUnavailable
	}
	compatibility, err := parseTyranoScriptCompatibility(source.CompatibilityJSON)
	if err != nil || source.AdapterKind != "TYRANOSCRIPT_WEB" || source.AdapterID != "tyranoscript-web" ||
		source.CoreID != "tyranoscript" || source.ContentKind != tyranoScriptProjectFormat ||
		!service.tyranoScriptBridgeAvailable(source.RuntimeVersion, compatibility.BridgePath) {
		return ErrReviewPreviewUnavailable
	}
	return nil
}

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
	var digest, format string
	err := service.database.QueryRowContext(ctx, `
SELECT blob.sha256,preview.content_format
FROM review_preview_sessions preview
JOIN blobs blob ON blob.id=preview.content_blob_id
WHERE preview.id=? AND preview.state='ACTIVE' AND preview.hard_expires_at_ms>?
 AND preview.content_format='TYRANOSCRIPT_PROJECT_V1' AND preview.content_logical_name=?
UNION ALL
SELECT blob.sha256,preview.content_format
FROM review_preview_sessions preview
JOIN review_preview_files file ON file.preview_session_id=preview.id
JOIN blobs blob ON blob.id=file.blob_id
WHERE preview.id=? AND preview.state='ACTIVE' AND preview.hard_expires_at_ms>?
 AND preview.content_format='TYRANOSCRIPT_PROJECT_V1' AND file.role='PROJECT_FILE' AND file.logical_name=?
`, sessionID, service.now().UnixMilli(), logicalName,
		sessionID, service.now().UnixMilli(), logicalName).Scan(&digest, &format)
	if err != nil || format != tyranoScriptProjectFormat {
		return ContentView{}, ErrCredential
	}
	return ContentView{Digest: digest, Format: format, CoreID: "tyranoscript", PlatformKey: "tyranoscript"}, nil
}

func (service *Service) TyranoScriptBridgeAuthorized(
	ctx context.Context,
	sessionID string,
	preview bool,
) (string, string, error) {
	var runtimeVersion, compatibilityJSON, adapterKind, family, state string
	var hardExpires int64
	query := `
SELECT artifact.runtime_version,artifact.compatibility_json,artifact.runtime_adapter_kind,
 artifact.runtime_family,session.state,session.hard_expires_at_ms
FROM launch_sessions session JOIN core_artifacts artifact ON artifact.id=session.core_artifact_id
WHERE session.id=? AND artifact.available_for_launch=1`
	if preview {
		query = `
SELECT artifact.runtime_version,artifact.compatibility_json,artifact.runtime_adapter_kind,
 artifact.runtime_family,session.state,session.hard_expires_at_ms
FROM review_preview_sessions session JOIN core_artifacts artifact ON artifact.id=session.core_artifact_id
WHERE session.id=? AND artifact.available_for_launch=1`
	}
	err := service.database.QueryRowContext(ctx, query, sessionID).Scan(
		&runtimeVersion, &compatibilityJSON, &adapterKind, &family, &state, &hardExpires,
	)
	compatibility, parseErr := parseTyranoScriptCompatibility(compatibilityJSON)
	if err != nil || parseErr != nil || state != "ACTIVE" || hardExpires <= service.now().UnixMilli() ||
		family != "TYRANOSCRIPT" || adapterKind != "TYRANOSCRIPT_WEB" ||
		!service.tyranoScriptBridgeAvailable(runtimeVersion, compatibility.BridgePath) {
		return "", "", ErrCredential
	}
	return runtimeVersion, compatibility.BridgePath, nil
}
