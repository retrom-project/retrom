package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
)

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) review(writer http.ResponseWriter, request *http.Request) {
	var itemID, importJobID, metadata, platformID, platformName, sourceSnapshotID, sourceManifest string
	var sourceContentKind, currentArtifactCompatibility string
	var validationID, validationStatus, compatibilityCode, dependencySnapshot sql.NullString
	var selectedValidationID sql.NullString
	var validationGeneration sql.NullInt64
	var selectedCandidateID, coverID, uploadedCoverID, backgroundID, defaultDOSEntry sql.NullString
	var version, updatedAtMS int64
	err := server.database.QueryRowContext(request.Context(), `
SELECT i.id,
i.import_job_id,
d.metadata_json,
d.version,
d.updated_at_ms,
pi.id,
pi.name,
current_artifact.compatibility_config_json,
v.id,
v.status,
v.compatibility_code,
v.dependency_snapshot_json,
d.selected_validation_id,
source_snapshot.id,
source_snapshot.source_manifest_json,
source_snapshot.content_kind,
v.prepublish_generation,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.cover_uploaded_asset_id,
d.background_candidate_asset_id,
d.default_dos_entry
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=d.effective_source_snapshot_id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
JOIN core_artifacts current_artifact ON current_artifact.core_id=pi.default_core_id
AND current_artifact.enabled=1
LEFT
JOIN import_item_core_validations v ON v.id=(
  SELECT candidate.id
FROM import_item_core_validations candidate
WHERE candidate.import_item_id=i.id
AND candidate.source_snapshot_id=d.effective_source_snapshot_id
AND candidate.target_platform_instance_id=d.target_platform_instance_id
AND candidate.core_artifact_id=current_artifact.id
ORDER BY candidate.created_at_ms DESC,
candidate.id DESC LIMIT 1)
WHERE i.id=?
AND i.state='REVIEW_PENDING'
AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items pegasus
  WHERE pegasus.library_import_item_id=i.id AND pegasus.execution_state<>'REVIEW_PENDING'
)
`, request.PathValue("importItemId")).
		Scan(
			&itemID,
			&importJobID,
			&metadata,
			&version,
			&updatedAtMS,
			&platformID,
			&platformName,
			&currentArtifactCompatibility,
			&validationID,
			&validationStatus,
			&compatibilityCode,
			&dependencySnapshot,
			&selectedValidationID,
			&sourceSnapshotID,
			&sourceManifest,
			&sourceContentKind,
			&validationGeneration,
			&selectedCandidateID,
			&coverID,
			&uploadedCoverID,
			&backgroundID,
			&defaultDOSEntry,
		)
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var metadataValue, sourceValue any
	_ = json.Unmarshal([]byte(metadata), &metadataValue)
	_ = json.Unmarshal([]byte(sourceManifest), &sourceValue)
	if files, ok := sourceValue.([]any); ok {
		sourceValue = map[string]any{"files": files}
	}
	dependencyValue := decodeOptionalJSON(dependencySnapshot)
	evidence, err := server.loadReviewEvidence(request, itemID, sourceSnapshotID, reviewValidationInput{
		validationID: validationID, validationStatus: validationStatus,
		compatibilityCode: compatibilityCode, dependencyValue: dependencyValue,
		validationGeneration: validationGeneration, selectedValidationID: selectedValidationID,
		artifactCompatibility: currentArtifactCompatibility, sourceContentKind: sourceContentKind,
	})
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	gateReviewMultiDiscAttachment(evidence.multiDisc, evidence.validation.stale)
	canApprove := evidence.validation.canApprove || evidence.runtimeScreenshot.value != nil
	reviewTags, err := server.activeReviewTags(request.Context(), itemID)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(writer, http.StatusOK, map[string]any{
		"itemId": itemID, "importJobId": importJobID, "version": version, "updatedAtMs": updatedAtMS,
		"effectiveSourceSnapshotId": sourceSnapshotID,
		"platformInstance": map[string]any{
			"id":   platformID,
			"name": platformName,
		}, "metadata": metadataValue, "sourceManifest": sourceValue,
		"validation": evidence.validation.value, "candidates": evidence.candidates,
		"scrapeRuns":                   evidence.scrapeRuns,
		"validationStale":              evidence.validation.stale,
		"selectedValidationGeneration": evidence.validation.selectedGeneration,
		"canApprove":                   canApprove,
		"uploadedAssets":               evidence.uploadedAssets, "sourceFiles": evidence.sourceFiles,
		"sourceMedia":       evidence.sourceMedia.value,
		"runtimeScreenshot": evidence.runtimeScreenshot.value,
		"duplicateGames":    evidence.duplicateGames, "contentIdentityDigest": evidence.contentIdentityDigest,
		"arcadeDependencies":  evidence.arcadeDependencies,
		"multiDisc":           evidence.multiDisc,
		"selectedCandidateId": nullableString(selectedCandidateID),
		"defaultDosEntry":     nullableString(defaultDOSEntry),
		"selectedAssets": map[string]any{
			"coverCandidateAssetId":       nullableString(coverID),
			"coverUploadedAssetId":        nullableString(uploadedCoverID),
			"backgroundCandidateAssetId":  nullableString(backgroundID),
			"screenshotCandidateAssetIds": evidence.screenshotIDs,
		}, "dosEntries": evidence.dosEntries, "tags": reviewTags,
	})
}

type reviewEvidence struct {
	candidates            []map[string]any
	scrapeRuns            []map[string]any
	uploadedAssets        []map[string]any
	sourceFiles           []map[string]any
	screenshotIDs         []string
	dosEntries            []map[string]any
	sourceMedia           optionalReviewProjection
	runtimeScreenshot     optionalReviewProjection
	duplicateGames        any
	contentIdentityDigest string
	arcadeDependencies    any
	multiDisc             any
	validation            reviewValidationResult
}

func (server *Server) loadReviewEvidence(
	request *http.Request,
	itemID, sourceSnapshotID string,
	validationInput reviewValidationInput,
) (reviewEvidence, error) {
	var evidence reviewEvidence
	var err error
	evidence.candidates, evidence.scrapeRuns, err = server.reviewMetadataEvidence(request, itemID)
	if err != nil {
		return reviewEvidence{}, err
	}
	evidence.uploadedAssets, err = server.reviewUploadedAssets(request, itemID)
	if err != nil {
		return reviewEvidence{}, err
	}
	evidence.sourceMedia, err = server.optionalReviewServerSourceMedia(request.Context(), itemID)
	if err != nil {
		return reviewEvidence{}, err
	}
	evidence.sourceFiles, err = server.reviewSourceFiles(request, sourceSnapshotID)
	if err != nil {
		return reviewEvidence{}, err
	}
	evidence.screenshotIDs, err = server.reviewScreenshotIDs(request.Context(), itemID)
	if err != nil {
		return reviewEvidence{}, err
	}
	evidence.dosEntries, err = server.reviewDOSEntries(request.Context(), itemID)
	if err != nil {
		return reviewEvidence{}, err
	}
	evidence.duplicateGames, evidence.contentIdentityDigest, err = server.importer.DuplicateGames(
		request.Context(), itemID,
	)
	if err != nil {
		return reviewEvidence{}, fmt.Errorf("load review duplicate games: %w", err)
	}
	evidence.arcadeDependencies, evidence.multiDisc, err = server.reviewContentDependencies(
		request.Context(), itemID,
	)
	if err != nil {
		return reviewEvidence{}, err
	}
	evidence.validation, evidence.runtimeScreenshot, err = server.reviewValidationEvidence(
		request.Context(), itemID, validationInput,
	)
	return evidence, err
}

type optionalReviewProjection struct {
	value any
}

func (server *Server) reviewRuntimeScreenshot(
	ctx context.Context,
	itemID string,
	validationID sql.NullString,
	validationCurrent bool,
) (optionalReviewProjection, error) {
	if !validationCurrent || !validationID.Valid {
		return optionalReviewProjection{}, nil
	}
	var id, coreArtifactID string
	var width, height, capturedAtMS int64
	err := server.database.QueryRowContext(ctx, `
SELECT screenshot.id,screenshot.core_artifact_id,screenshot.width_px,screenshot.height_px,screenshot.captured_at_ms
FROM review_runtime_screenshots screenshot
JOIN review_drafts draft ON draft.import_item_id=screenshot.import_item_id
WHERE screenshot.import_item_id=? AND screenshot.validation_id=?
AND screenshot.source_snapshot_id=draft.effective_source_snapshot_id
`, itemID, validationID.String).Scan(&id, &coreArtifactID, &width, &height, &capturedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return optionalReviewProjection{}, nil
	}
	if err != nil {
		return optionalReviewProjection{}, fmt.Errorf("review runtime screenshot: %w", err)
	}
	return optionalReviewProjection{value: map[string]any{
		"screenshotId": id, "validationId": validationID.String, "coreArtifactId": coreArtifactID,
		"widthPx": width, "heightPx": height, "capturedAfterMs": int64(5_000),
		"capturedAtMs": capturedAtMS, "url": "/api/v1/admin/review-assets/" + id,
	}}, nil
}

func (server *Server) optionalReviewServerSourceMedia(
	ctx context.Context,
	itemID string,
) (optionalReviewProjection, error) {
	value, found, err := server.reviewServerSourceMedia(ctx, itemID)
	if err != nil {
		return optionalReviewProjection{}, err
	}
	if !found {
		return optionalReviewProjection{}, nil
	}
	return optionalReviewProjection{value: value}, nil
}

func (server *Server) reviewServerSourceMedia(ctx context.Context, itemID string) (any, bool, error) {
	var sourceRefID, importID, collectionName string
	var hasCover, hasVideo int
	var coverWidth, coverHeight sql.NullInt64
	err := server.database.QueryRowContext(ctx, `
SELECT pegasus.id,pegasus.import_id,COALESCE(collection.name,''),
EXISTS(
 SELECT 1 FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='COVER' AND asset.state='COPIED'
AND asset.blob_id IS NOT NULL
),
(
 SELECT asset.width_px FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='COVER' AND asset.state='COPIED'
),
(
 SELECT asset.height_px FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='COVER' AND asset.state='COPIED'
),
EXISTS(
 SELECT 1 FROM pegasus_import_item_assets asset
 WHERE asset.item_id=pegasus.id AND asset.kind='VIDEO' AND asset.state='COPIED'
 AND asset.blob_id IS NOT NULL
)
FROM pegasus_import_items pegasus
LEFT JOIN pegasus_import_collections collection ON collection.id=pegasus.collection_id
WHERE pegasus.library_import_item_id=?
`, itemID).Scan(
		&sourceRefID, &importID, &collectionName, &hasCover, &coverWidth, &coverHeight, &hasVideo,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("review source media: %w", err)
	}
	sourceLabel := any(nil)
	if collectionName != "" {
		sourceLabel = collectionName
	}
	result := map[string]any{
		"sourceKind": "PEGASUS", "sourceRefId": sourceRefID, "pegasusImportId": importID,
		"sourceLabel": sourceLabel, "coverUrl": nil, "coverWidthPx": nil, "coverHeightPx": nil,
		"videoUrl": nil,
	}
	baseURL := "/api/v1/admin/review-assets/" + sourceRefID
	if hasCover == 1 {
		result["coverUrl"] = baseURL + "?kind=COVER"
		result["coverWidthPx"] = nullableInt64(coverWidth)
		result["coverHeightPx"] = nullableInt64(coverHeight)
	}
	if hasVideo == 1 {
		result["videoUrl"] = baseURL + "?kind=VIDEO"
	}
	return result, true, nil
}

type reviewValidationInput struct {
	validationID, validationStatus, compatibilityCode, selectedValidationID sql.NullString
	validationGeneration                                                    sql.NullInt64
	dependencyValue                                                         any
	artifactCompatibility, sourceContentKind                                string
}

type reviewValidationResult struct {
	value, selectedGeneration any
	stale, canApprove         bool
}

func (server *Server) reviewValidationProjection(
	ctx context.Context,
	input reviewValidationInput,
) (reviewValidationResult, error) {
	if !input.validationID.Valid {
		return reviewValidationResult{}, nil
	}
	evidenceCurrent, err := server.importer.ReviewValidationCurrent(ctx, input.validationID.String)
	if err != nil {
		return reviewValidationResult{}, fmt.Errorf("review validation projection: %w", err)
	}
	selectedGeneration := any(nil)
	if input.selectedValidationID.Valid {
		selectedGeneration = nullableInt64(input.validationGeneration)
	}
	return reviewValidationResult{
		value: map[string]any{
			"id": input.validationID.String, "status": input.validationStatus.String,
			"current":            evidenceCurrent && input.validationStatus.String == "READY",
			"generation":         nullableInt64(input.validationGeneration),
			"compatibilityCode":  input.compatibilityCode.String,
			"dependencySnapshot": input.dependencyValue,
		},
		selectedGeneration: selectedGeneration,
		stale:              !evidenceCurrent,
		canApprove: input.selectedValidationID.Valid && evidenceCurrent && input.validationStatus.String == "READY" &&
			contentcapability.SupportsContentKind(input.artifactCompatibility, input.sourceContentKind),
	}, nil
}

func (server *Server) reviewValidationEvidence(
	ctx context.Context,
	itemID string,
	input reviewValidationInput,
) (reviewValidationResult, optionalReviewProjection, error) {
	projection, err := server.reviewValidationProjection(ctx, input)
	if err != nil {
		return reviewValidationResult{}, optionalReviewProjection{}, err
	}
	screenshot, err := server.reviewRuntimeScreenshot(ctx, itemID, input.validationID, !projection.stale)
	if err != nil {
		return reviewValidationResult{}, optionalReviewProjection{}, err
	}
	return projection, screenshot, nil
}

func (server *Server) reviewContentDependencies(ctx context.Context, itemID string) (any, any, error) {
	arcadeDependencies, hasArcadeDependencies, err := server.importer.ReviewArcadeDependencies(ctx, itemID)
	if err != nil {
		return nil, nil, fmt.Errorf("review arcade dependencies: %w", err)
	}
	if !hasArcadeDependencies {
		arcadeDependencies = nil
	}
	multiDisc, hasMultiDisc, err := server.importer.ReviewMultiDisc(ctx, itemID)
	if err != nil {
		return nil, nil, fmt.Errorf("review multi-disc dependencies: %w", err)
	}
	if !hasMultiDisc {
		multiDisc = nil
	}
	return arcadeDependencies, multiDisc, nil
}

func gateReviewMultiDiscAttachment(value any, validationStale bool) {
	projection, ok := value.(map[string]any)
	if !ok {
		return
	}
	canAttach, ok := projection["canAttachMissingDiscs"].(bool)
	if !ok {
		projection["canAttachMissingDiscs"] = false
		return
	}
	projection["canAttachMissingDiscs"] = canAttach && !validationStale
}

func (server *Server) reviewMetadataEvidence(
	request *http.Request,
	itemID string,
) ([]map[string]any, []map[string]any, error) {
	candidates, err := server.reviewCandidates(request, itemID)
	if err != nil {
		return nil, nil, err
	}
	runs, err := server.reviewScrapeRuns(request, itemID)
	if err != nil {
		return nil, nil, err
	}
	return candidates, runs, nil
}

func (server *Server) reviewScreenshotIDs(ctx context.Context, itemID string) ([]string, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT s.candidate_asset_id
FROM review_draft_screenshot_assets s
JOIN review_drafts d ON d.id=s.review_draft_id
WHERE d.import_item_id=?
ORDER BY s.ordinal
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review screenshots: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan review screenshot: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review screenshots: %w", err)
	}
	return ids, nil
}

func (server *Server) reviewDOSEntries(ctx context.Context, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT normalized_path,
original_relative_path,
kind,
rank,
enabled,
direct_launch_safe
FROM import_item_dos_entries
WHERE import_item_id=?
ORDER BY rank,normalized_path
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review DOS entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0)
	for rows.Next() {
		var path, originalPath, kind string
		var rank, enabled, directSafe int64
		if err := rows.Scan(&path, &originalPath, &kind, &rank, &enabled, &directSafe); err != nil {
			return nil, fmt.Errorf("scan review DOS entry: %w", err)
		}
		entries = append(entries, map[string]any{
			"path": path, "originalPath": originalPath, "kind": kind, "rank": rank,
			"enabled": enabled == 1, "directLaunchSafe": directSafe == 1,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review DOS entries: %w", err)
	}
	return entries, nil
}

func (server *Server) reviewUploadedAssets(request *http.Request, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(request.Context(), `
SELECT id,kind,width_px,height_px,media_type,created_at_ms
FROM review_uploaded_assets
WHERE import_item_id=?
ORDER BY created_at_ms,id
`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query uploaded review assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	assets := make([]map[string]any, 0)
	for rows.Next() {
		var id, kind, mediaType string
		var width, height, createdAtMS int64
		if err := rows.Scan(&id, &kind, &width, &height, &mediaType, &createdAtMS); err != nil {
			return nil, fmt.Errorf("scan uploaded review asset: %w", err)
		}
		assets = append(assets, map[string]any{
			"assetId": id, "kind": kind, "widthPx": width, "heightPx": height,
			"mediaType": mediaType, "url": "/api/v1/admin/review-assets/" + id, "createdAtMs": createdAtMS,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan uploaded review assets: %w", err)
	}
	return assets, nil
}

func (server *Server) reviewSourceFiles(request *http.Request, sourceSnapshotID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(request.Context(), `
SELECT f.id,f.relative_path,b.size_bytes,b.sha256,b.md5,b.crc32,
MAX(CASE WHEN s.source_archive_blob_id IS NOT NULL OR EXISTS(
  SELECT 1 FROM archive_entries ae WHERE ae.archive_blob_id=f.final_blob_id
) THEN 1 ELSE 0 END),
COALESCE(
  MAX(s.source_archive_blob_id),
  MAX(CASE WHEN EXISTS(
    SELECT 1 FROM archive_entries ae WHERE ae.archive_blob_id=f.final_blob_id
  ) THEN f.final_blob_id END)
)
FROM import_item_source_snapshot_files s
JOIN upload_files f ON f.id=s.upload_file_id
JOIN blobs b ON b.id=f.final_blob_id
WHERE s.source_snapshot_id=?
GROUP BY f.id,f.relative_path,b.size_bytes,b.sha256,b.md5,b.crc32
ORDER BY min(s.sort_order),f.relative_path,f.id
`, sourceSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("query review source files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type sourceRow struct {
		id, name, sha256, md5, crc32 string
		size                         int64
		archive                      int64
		archiveBlobID                sql.NullString
	}
	records := make([]sourceRow, 0)
	for rows.Next() {
		var record sourceRow
		if err := rows.Scan(&record.id, &record.name, &record.size, &record.sha256, &record.md5,
			&record.crc32, &record.archive, &record.archiveBlobID); err != nil {
			return nil, fmt.Errorf("scan review source file: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review source files: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close review source files: %w", err)
	}
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		var entries []map[string]any
		var archiveFormat any
		if record.archiveBlobID.Valid {
			entries, archiveFormat, err = server.reviewArchiveEntries(request.Context(), record.archiveBlobID.String)
			if err != nil {
				return nil, err
			}
		} else {
			entries = make([]map[string]any, 0)
		}
		result = append(result, map[string]any{
			"uploadFileId": record.id, "name": record.name, "sizeBytes": record.size,
			"sha256": record.sha256, "md5": record.md5, "crc32": record.crc32,
			"archive": record.archive == 1, "archiveFormat": archiveFormat, "archiveEntries": entries,
		})
	}
	return result, nil
}

func (server *Server) reviewArchiveEntries(ctx context.Context, archiveBlobID string) ([]map[string]any, any, error) {
	rows, err := server.database.QueryContext(ctx, `
SELECT original_relative_path,uncompressed_size_bytes,crc32,archive_format
FROM archive_entries
WHERE archive_blob_id=?
ORDER BY ordinal
`, archiveBlobID)
	if err != nil {
		return nil, nil, fmt.Errorf("query review archive entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0)
	var archiveFormat any
	for rows.Next() {
		var name, crc32, format string
		var size int64
		if err := rows.Scan(&name, &size, &crc32, &format); err != nil {
			return nil, nil, fmt.Errorf("scan review archive entry: %w", err)
		}
		archiveFormat = format
		entries = append(entries, map[string]any{"name": name, "sizeBytes": size, "crc32": crc32})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan review archive entries: %w", err)
	}
	return entries, archiveFormat, nil
}

func (server *Server) reviewScrapeRuns(request *http.Request, itemID string) ([]map[string]any, error) {
	rows, err := server.database.QueryContext(
		request.Context(),
		`
WITH evidence_counts AS (
  SELECT scrape_run_id,
  COUNT(*) AS evidence_count
  FROM content_hash_evidence
  GROUP BY scrape_run_id
), candidate_counts AS (
  SELECT scrape_run_id,
  COUNT(*) AS candidate_count
  FROM scrape_candidates
  GROUP BY scrape_run_id
), outcome_counts AS (
  SELECT a.scrape_run_id,
  COUNT(*) AS attempt_count,
  SUM(CASE WHEN p.outcome='HIT' THEN 1 ELSE 0 END) AS hit,
  SUM(CASE WHEN p.outcome='MISS' THEN 1 ELSE 0 END) AS miss,
  SUM(CASE WHEN p.outcome='RATE_LIMITED' THEN 1 ELSE 0 END) AS rate_limited,
  SUM(CASE WHEN p.outcome='TIMEOUT' THEN 1 ELSE 0 END) AS timeout,
  SUM(CASE WHEN p.outcome='INVALID_RESPONSE' THEN 1 ELSE 0 END) AS invalid_response,
  SUM(CASE WHEN p.outcome='NETWORK_ERROR' THEN 1 ELSE 0 END) AS network_error
  FROM metadata_scrape_query_attempts a
  JOIN metadata_provider_responses p ON p.id=a.provider_response_id
  GROUP BY a.scrape_run_id
)
SELECT r.id,
r.job_id,
r.provider,
r.state,
j.state,
r.created_at_ms,
r.completed_at_ms,
r.error_code,
COALESCE(e.evidence_count,0),
COALESCE(o.attempt_count,0),
COALESCE(c.candidate_count,0),
COALESCE(o.hit,0),
COALESCE(o.miss,0),
COALESCE(o.rate_limited,0),
COALESCE(o.timeout,0),
COALESCE(o.invalid_response,0),
COALESCE(o.network_error,0)
FROM metadata_scrape_runs r
JOIN jobs j ON j.id=r.job_id
LEFT JOIN evidence_counts e ON e.scrape_run_id=r.id
LEFT JOIN candidate_counts c ON c.scrape_run_id=r.id
LEFT JOIN outcome_counts o ON o.scrape_run_id=r.id
WHERE r.import_item_id=?
ORDER BY r.created_at_ms DESC,
r.id DESC
LIMIT 10
`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("query review scrape runs: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	runs := make([]map[string]any, 0)
	for rows.Next() {
		run, scanErr := scanReviewScrapeRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan review scrape runs: %w", err)
	}
	return runs, nil
}
