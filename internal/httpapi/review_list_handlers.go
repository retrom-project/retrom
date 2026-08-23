package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/cursor"
	"retrom/internal/tagging"
)

var (
	errInvalidReviewQuery  = errors.New("invalid review query")
	errInvalidReviewCursor = errors.New("invalid review cursor")
)

type reviewListSpec struct {
	query, sortCode, filterDigest string
	arguments                     []any
	limit                         int
}

func validReviewQueryKeys(values url.Values) bool {
	allowed := map[string]struct{}{
		"q": {}, "tagId": {}, "importJobId": {}, "pegasusImportId": {},
		"emulationStationImportId": {}, "platformInstanceId": {},
		"blockerCode": {}, "sort": {}, "cursor": {}, "limit": {},
	}
	for key := range values {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func applyReviewListFilters(query string, values url.Values) (string, []any, string, error) {
	arguments := make([]any, 0, 8)
	sourceFilterCount := 0
	for _, key := range []string{"importJobId", "pegasusImportId", "emulationStationImportId"} {
		if values.Get(key) != "" {
			sourceFilterCount++
		}
	}
	if sourceFilterCount > 1 {
		return "", nil, "", errInvalidReviewQuery
	}
	if importJobID := values.Get("importJobId"); importJobID != "" {
		query += " AND i.import_job_id=?"
		arguments = append(arguments, importJobID)
	}
	if pegasusImportID := values.Get("pegasusImportId"); pegasusImportID != "" {
		query += " AND pegasus.import_id=?"
		arguments = append(arguments, pegasusImportID)
	}
	if importID := values.Get("emulationStationImportId"); importID != "" {
		query += " AND emulationstation.import_id=?"
		arguments = append(arguments, importID)
	}
	normalizedQ := strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " "))
	if len([]rune(normalizedQ)) > 200 {
		return "", nil, "", errInvalidReviewQuery
	}
	if normalizedQ != "" {
		query += ` AND (instr(i.search_text,?)>0 OR EXISTS(
 SELECT 1 FROM review_draft_tags relation
 JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND instr(tag.name_key,?)>0
))`
		arguments = append(arguments, normalizedQ, normalizedQ)
	}
	tagID := values.Get("tagId")
	if tagID != "" {
		if !tagging.ValidID(tagID) {
			return "", nil, "", errInvalidReviewQuery
		}
		query += ` AND EXISTS(
 SELECT 1 FROM review_draft_tags relation
 JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND tag.id=?)`
		arguments = append(arguments, tagID)
	}
	if value := values.Get("platformInstanceId"); value != "" {
		query += " AND d.target_platform_instance_id=?"
		arguments = append(arguments, value)
	}
	if value := values.Get("blockerCode"); value != "" {
		query += " AND (v.compatibility_code=? OR (?='NEEDS_VALIDATION' AND v.id IS NULL))"
		arguments = append(arguments, value, value)
	}
	return query, arguments, normalizedQ, nil
}

func reviewListSortCode(values url.Values) (string, error) {
	sortCode := values.Get("sort")
	if sortCode == "" {
		sortCode = "UPDATED_ASC"
	}
	if sortCode != "UPDATED_ASC" && sortCode != "UPDATED_DESC" {
		return "", errInvalidReviewQuery
	}
	return sortCode, nil
}

func reviewListLimit(values url.Values) (int, error) {
	raw := values.Get("limit")
	if raw == "" {
		return 20, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 || parsed > 20 {
		return 0, errInvalidReviewQuery
	}
	return parsed, nil
}

func (server *Server) applyReviewListCursor(
	spec *reviewListSpec,
	token string,
) error {
	if token == "" {
		return nil
	}
	payload, err := server.cursors.Decode(token, "getAdminReviews", spec.filterDigest, spec.sortCode)
	if err != nil || len(payload.SortValues) != 1 {
		return errInvalidReviewCursor
	}
	updatedAt, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
	if err != nil {
		return errInvalidReviewCursor
	}
	if spec.sortCode == "UPDATED_ASC" {
		spec.query += " AND (d.updated_at_ms>? OR (d.updated_at_ms=? AND i.id>?))"
	} else {
		spec.query += " AND (d.updated_at_ms<? OR (d.updated_at_ms=? AND i.id<?))"
	}
	spec.arguments = append(spec.arguments, updatedAt, updatedAt, payload.ID)
	return nil
}

func (server *Server) prepareReviewList(
	query string,
	values url.Values,
	principalID string,
) (reviewListSpec, error) {
	if !validReviewQueryKeys(values) {
		return reviewListSpec{}, errInvalidReviewQuery
	}
	query, arguments, normalizedQ, err := applyReviewListFilters(query, values)
	if err != nil {
		return reviewListSpec{}, err
	}
	sortCode, err := reviewListSortCode(values)
	if err != nil {
		return reviewListSpec{}, err
	}
	limit, err := reviewListLimit(values)
	if err != nil {
		return reviewListSpec{}, err
	}
	spec := reviewListSpec{
		query: query, arguments: arguments, sortCode: sortCode, limit: limit,
		filterDigest: cursor.FilterDigest(map[string]any{
			"principalId": principalID, "q": normalizedQ, "tagId": values.Get("tagId"),
			"importJobId": values.Get("importJobId"), "pegasusImportId": values.Get("pegasusImportId"),
			"emulationStationImportId": values.Get("emulationStationImportId"),
			"platformInstanceId":       values.Get("platformInstanceId"), "blockerCode": values.Get("blockerCode"),
		}),
	}
	if err := server.applyReviewListCursor(&spec, values.Get("cursor")); err != nil {
		return reviewListSpec{}, err
	}
	if sortCode == "UPDATED_ASC" {
		spec.query += " ORDER BY d.updated_at_ms ASC,i.id ASC LIMIT ?"
	} else {
		spec.query += " ORDER BY d.updated_at_ms DESC,i.id DESC LIMIT ?"
	}
	spec.arguments = append(spec.arguments, limit+1)
	return spec, nil
}

// Contract branches stay contiguous for a single auditable decision.
func (server *Server) reviews(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	query := `
SELECT i.id,
d.version,
i.import_job_id,
json_extract(d.metadata_json,
'$.title'),
COALESCE(json_extract(i.source_manifest_json,
'$[0].logicalName'),
json_extract(i.source_manifest_json,
'$.files[0].logicalName'),
json_extract(d.metadata_json,
'$.title')),
pi.id,
pi.name,
v.status,
v.compatibility_code,
i.updated_at_ms,
(SELECT count(*)
FROM scrape_candidates c
JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
WHERE r.import_item_id=i.id
AND r.state='COMPLETED'),
(SELECT COALESCE(sum(b.size_bytes),0)
 FROM import_item_source_snapshot_files source_file
 JOIN blobs b ON b.id=source_file.blob_id
 WHERE source_file.source_snapshot_id=d.effective_source_snapshot_id),
(SELECT b.md5
 FROM import_item_source_snapshot_files source_file
 JOIN blobs b ON b.id=source_file.blob_id
 WHERE source_file.source_snapshot_id=d.effective_source_snapshot_id
 ORDER BY CASE source_file.role WHEN 'CONTENT' THEN 0 WHEN 'DOS_SOURCE' THEN 1 ELSE 2 END,
 source_file.sort_order,
 source_file.logical_name
 LIMIT 1),
COALESCE(d.cover_uploaded_asset_id,(SELECT asset.id
 FROM scrape_candidate_assets asset
 JOIN scrape_candidates candidate ON candidate.id=asset.scrape_candidate_id
 JOIN metadata_scrape_runs run ON run.id=candidate.scrape_run_id
 WHERE run.import_item_id=i.id
 AND run.state='COMPLETED'
 AND asset.status='READY'
 AND asset.kind_hint='COVER'
 ORDER BY CASE WHEN asset.id=d.cover_candidate_asset_id THEN 0 ELSE 1 END,
 run.completed_at_ms DESC,
 asset.ordinal,
 asset.id
 LIMIT 1))
,pegasus.id,pegasus.import_id,pegasus_collection.name,
EXISTS(
 SELECT 1 FROM pegasus_import_item_assets pegasus_asset
 WHERE pegasus_asset.item_id=pegasus.id AND pegasus_asset.kind='COVER'
 AND pegasus_asset.state='COPIED' AND pegasus_asset.blob_id IS NOT NULL
),emulationstation.id,emulationstation.import_id,emulationstation_collection.display_name,
EXISTS(
 SELECT 1 FROM emulationstation_import_item_assets source_asset
 WHERE source_asset.item_id=emulationstation.id AND source_asset.kind='COVER'
 AND source_asset.state='COPIED' AND source_asset.blob_id IS NOT NULL
)
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
	LEFT JOIN pegasus_import_items pegasus ON pegasus.library_import_item_id=i.id
	LEFT JOIN pegasus_import_collections pegasus_collection ON pegasus_collection.id=pegasus.collection_id
	LEFT JOIN emulationstation_import_items emulationstation
	 ON emulationstation.library_import_item_id=i.id
	LEFT JOIN emulationstation_import_collections emulationstation_collection
	 ON emulationstation_collection.id=emulationstation.collection_id
	LEFT
JOIN import_item_core_validations v ON v.id=COALESCE(d.selected_validation_id,
(SELECT candidate.id
FROM import_item_core_validations candidate
WHERE candidate.import_item_id=i.id
AND candidate.source_snapshot_id=d.effective_source_snapshot_id
AND candidate.target_platform_instance_id=d.target_platform_instance_id
ORDER BY candidate.created_at_ms DESC,
candidate.id DESC LIMIT 1))
WHERE i.state='REVIEW_PENDING'
AND (i.review_handoff_kind='DIRECT' OR
  emulationstation.execution_state='REVIEW_PENDING')
AND (pegasus.id IS NULL OR pegasus.execution_state='REVIEW_PENDING')
AND (emulationstation.id IS NULL OR emulationstation.execution_state='REVIEW_PENDING')
`
	values := request.URL.Query()
	spec, err := server.prepareReviewList(query, values, principal.UserID)
	if errors.Is(err, errInvalidReviewCursor) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "待审核筛选无效", map[string]any{})
		return
	}
	rows, err := server.database.QueryContext(request.Context(), spec.query, spec.arguments...)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items, err := scanReviewListRows(rows, spec.limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	if err := projectMapTags(
		request.Context(), items, "itemId", server.tagService.ReviewReferences,
	); err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var nextCursor any
	if len(items) > spec.limit {
		last := items[spec.limit-1]
		items = items[:spec.limit]
		updatedAtMS, updatedOK := last["updatedAtMs"].(int64)
		lastID, idOK := last["itemId"].(string)
		if !updatedOK || !idOK {
			writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "审核分页投影无效", map[string]any{})
			return
		}
		token, err := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminReviews",
				FilterDigest: spec.filterDigest,
				SortCode:     spec.sortCode,
				SortValues:   []string{strconv.FormatInt(updatedAtMS, 10)},
				ID:           lastID,
			},
		)
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		nextCursor = token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func scanReviewListRows(rows *sql.Rows, capacity int) ([]map[string]any, error) {
	items := make([]map[string]any, 0, capacity)
	for rows.Next() {
		var itemID, importJobID, title, sourceName, platformID, platformName string
		var reviewVersion, updatedAtMS, candidateCount, sourceTotalSizeBytes int64
		var validationStatus, compatibilityCode, sourceMD5, coverAssetID sql.NullString
		var pegasusItemID, pegasusImportID, pegasusCollectionName sql.NullString
		var emulationStationItemID, emulationStationImportID, emulationStationCollectionName sql.NullString
		var hasPegasusCover, hasEmulationStationCover int
		if err := rows.Scan(
			&itemID,
			&reviewVersion,
			&importJobID,
			&title,
			&sourceName,
			&platformID,
			&platformName,
			&validationStatus,
			&compatibilityCode,
			&updatedAtMS,
			&candidateCount,
			&sourceTotalSizeBytes,
			&sourceMD5,
			&coverAssetID,
			&pegasusItemID,
			&pegasusImportID,
			&pegasusCollectionName,
			&hasPegasusCover,
			&emulationStationItemID,
			&emulationStationImportID,
			&emulationStationCollectionName,
			&hasEmulationStationCover,
		); err != nil {
			return nil, fmt.Errorf("scan review list item: %w", err)
		}
		blockers := []string{}
		if validationStatus.String != "READY" && compatibilityCode.Valid {
			blockers = append(blockers, compatibilityCode.String)
		}
		status := validationStatus.String
		if status == "" {
			status = "NEEDS_VALIDATION"
		}
		coverURL := reviewAssetURL(coverAssetID)
		if coverURL == nil && pegasusItemID.Valid && hasPegasusCover == 1 {
			coverURL = "/api/v1/admin/review-assets/" + pegasusItemID.String + "?kind=COVER"
		}
		if coverURL == nil && emulationStationItemID.Valid && hasEmulationStationCover == 1 {
			coverURL = "/api/v1/admin/review-assets/" + emulationStationItemID.String + "?kind=COVER"
		}
		sourceKind := "STANDARD"
		sourceLabel := nullableString(pegasusCollectionName)
		if pegasusImportID.Valid {
			sourceKind = "PEGASUS"
		} else if emulationStationImportID.Valid {
			sourceKind = "EMULATIONSTATION"
			sourceLabel = nullableString(emulationStationCollectionName)
		}
		items = append(
			items,
			map[string]any{
				"itemId":                   itemID,
				"reviewVersion":            reviewVersion,
				"importJobId":              importJobID,
				"sourceDisplayName":        sourceName,
				"draftTitle":               title,
				"platformInstance":         map[string]any{"id": platformID, "name": platformName},
				"validationStatus":         status,
				"validationJobId":          nil,
				"blockerCodes":             blockers,
				"candidateCount":           candidateCount,
				"sourceTotalSizeBytes":     sourceTotalSizeBytes,
				"sourceMd5":                nullableString(sourceMD5),
				"coverUrl":                 coverURL,
				"sourceKind":               sourceKind,
				"sourceLabel":              sourceLabel,
				"pegasusImportId":          nullableString(pegasusImportID),
				"emulationStationImportId": nullableString(emulationStationImportID),
				"updatedAtMs":              updatedAtMS,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review list items: %w", err)
	}
	return items, nil
}

func decodeOptionalJSON(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	var decoded any
	_ = json.Unmarshal([]byte(value.String), &decoded)
	return decoded
}

func (server *Server) activeReviewTags(ctx context.Context, itemID string) ([]tagging.Reference, error) {
	references, err := server.tagService.ReviewReferences(ctx, []string{itemID})
	if err != nil {
		return nil, fmt.Errorf("project review tags: %w", err)
	}
	tags := references[itemID]
	if tags == nil {
		tags = []tagging.Reference{}
	}
	return tags, nil
}
