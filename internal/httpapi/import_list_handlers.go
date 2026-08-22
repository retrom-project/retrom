package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/cursor"
	"retrom/internal/libraryimport"
	"retrom/internal/tagging"
)

type importOverviewSummary struct {
	Running         int64 `json:"running"`
	ReviewPending   int64 `json:"reviewPending"`
	PublishedItems  int64 `json:"publishedItems"`
	Completed       int64 `json:"completed"`
	Failed          int64 `json:"failed"`
	OrdinaryFailed  int64 `json:"ordinaryFailed"`
	PegasusFailed   int64 `json:"pegasusFailed"`
	ProcessingItems int64 `json:"processingItems"`
	IssueItems      int64 `json:"issueItems"`
}

func (server *Server) importSummary(writer http.ResponseWriter, request *http.Request) {
	var summary importOverviewSummary
	err := server.database.QueryRowContext(request.Context(), importOverviewSummarySQL).Scan(
		&summary.Running,
		&summary.ReviewPending,
		&summary.PublishedItems,
		&summary.Completed,
		&summary.Failed,
		&summary.OrdinaryFailed,
		&summary.PegasusFailed,
		&summary.ProcessingItems,
		&summary.IssueItems,
	)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, summary)
}

const userVisibleImportJobPredicate = `i.id NOT IN (
 SELECT pegasus_item.library_import_job_id FROM pegasus_import_items pegasus_item
 WHERE pegasus_item.library_import_job_id IS NOT NULL
)`

const importOverviewSummarySQL = `
WITH ordinary AS (
 SELECT i.state,i.total_item_count,i.failed_item_count,i.rejected_file_count,i.resolved_rejected_file_count
 FROM import_jobs i
 WHERE ` + userVisibleImportJobPredicate + `
)
SELECT
 (SELECT count(*) FROM ordinary WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))+
 (SELECT count(*) FROM pegasus_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')),
 (SELECT count(*) FROM import_items WHERE state='REVIEW_PENDING'),
 (SELECT count(*) FROM import_items WHERE state='PUBLISHED'),
 (SELECT count(*) FROM ordinary WHERE state='COMPLETED')+
 (SELECT count(*) FROM pegasus_imports WHERE state='COMPLETED'),
 (SELECT count(*) FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED'))+
 (SELECT count(*) FROM pegasus_imports WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 (SELECT count(*) FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 (SELECT count(*) FROM pegasus_imports WHERE state IN ('PARTIAL_FAILURE','FAILED')),
 COALESCE((SELECT sum(total_item_count) FROM ordinary
  WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')),0)+
 COALESCE((SELECT sum(game_count) FROM pegasus_imports
  WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')),0),
 COALESCE((SELECT sum(failed_item_count+CASE
   WHEN rejected_file_count>resolved_rejected_file_count
   THEN rejected_file_count-resolved_rejected_file_count ELSE 0 END)
  FROM ordinary WHERE state IN ('PARTIAL_FAILURE','FAILED')),0)+
 COALESCE((SELECT sum(blocked_item_count+failed_item_count) FROM pegasus_imports
  WHERE state IN ('PARTIAL_FAILURE','FAILED')),0)
`

type importListFilters struct {
	queryText   string
	state       string
	platformID  string
	digest      string
	sortCode    string
	cursorToken string
	limit       int
	sortField   string
}

func parseImportListFilters(values url.Values, principalID string) (importListFilters, error) {
	filters := importListFilters{
		queryText:   strings.ToLower(strings.Join(strings.Fields(values.Get("q")), " ")),
		state:       values.Get("state"),
		platformID:  values.Get("platformInstanceId"),
		sortCode:    values.Get("sort"),
		cursorToken: values.Get("cursor"),
		limit:       20,
		sortField:   "updatedAtMs",
	}
	if len([]rune(filters.queryText)) > 200 {
		return importListFilters{}, errQueryTooLong
	}
	if filters.state != "" && !validImportListState(filters.state) {
		return importListFilters{}, errUnknownQuery
	}
	if filters.sortCode == "" {
		filters.sortCode = "UPDATED_DESC"
	}
	if filters.sortCode == "CREATED_DESC" {
		filters.sortField = "createdAtMs"
	} else if filters.sortCode != "UPDATED_DESC" {
		return importListFilters{}, errUnknownQuery
	}
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 20 {
			return importListFilters{}, errInvalidLimit
		}
		filters.limit = parsed
	}
	filters.digest = cursor.FilterDigest(map[string]any{
		"principalId": principalID, "q": filters.queryText, "state": filters.state,
		"platformInstanceId": filters.platformID,
	})
	return filters, nil
}

func validImportListState(state string) bool {
	switch state {
	case "QUEUED",
		"RUNNING",
		"REVIEW_PENDING",
		"PARTIAL_FAILURE",
		"COMPLETED",
		"CANCEL_REQUESTED",
		"CANCELLED",
		"FAILED":
		return true
	default:
		return false
	}
}

func (server *Server) importListArguments(filters importListFilters) ([]any, error) {
	cursorID := ""
	cursorValue := int64(0)
	if filters.cursorToken != "" {
		payload, err := server.cursors.Decode(
			filters.cursorToken, "getAdminImports", filters.digest, filters.sortCode,
		)
		if err != nil || len(payload.SortValues) != 1 {
			return nil, errInvalidCursorPayload
		}
		parsed, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
		if err != nil {
			return nil, errInvalidCursorPayload
		}
		cursorID, cursorValue = payload.ID, parsed
	}
	return []any{
		filters.queryText, filters.queryText, filters.queryText,
		filters.state, filters.state,
		filters.platformID, filters.platformID,
		cursorID,
		filters.sortCode, cursorValue, cursorValue, cursorID,
		filters.sortCode, cursorValue, cursorValue, cursorID,
		filters.sortCode,
		filters.limit + 1,
	}, nil
}

const importListSQL = `
SELECT i.id,
i.state,
pi.name,
i.metadata_provider,
coalesce(json_extract(i.config_snapshot_json,'$.contentMode'),'STANDARD'),
i.total_item_count,
i.review_pending_item_count,
i.failed_item_count,
i.rejected_file_count,
i.resolved_rejected_file_count,
i.already_imported_item_count,
i.already_imported_file_count,
i.version,
i.created_at_ms,
i.updated_at_ms
FROM import_jobs i
JOIN platform_instances pi ON pi.id=i.target_platform_instance_id
WHERE ` + userVisibleImportJobPredicate + `
AND (?='' OR instr(lower(i.id),lower(?))>0 OR instr(lower(pi.name),lower(?))>0)
AND (?='' OR i.state=?)
AND (?='' OR i.target_platform_instance_id=?)
AND (?='' OR
(?='UPDATED_DESC' AND (i.updated_at_ms<? OR (i.updated_at_ms=? AND i.id<?))) OR
(?='CREATED_DESC' AND (i.created_at_ms<? OR (i.created_at_ms=? AND i.id<?))))
ORDER BY CASE ? WHEN 'UPDATED_DESC' THEN i.updated_at_ms WHEN 'CREATED_DESC' THEN i.created_at_ms END DESC,
i.id DESC
LIMIT ?
`

type importListItem struct {
	ID                          string `json:"id"`
	State                       string `json:"state"`
	PlatformInstanceName        string `json:"platformInstanceName"`
	MetadataProvider            string `json:"metadataProvider"`
	ContentMode                 string `json:"contentMode"`
	TotalItemCount              int64  `json:"totalItemCount"`
	ReviewPendingItemCount      int64  `json:"reviewPendingItemCount"`
	FailedItemCount             int64  `json:"failedItemCount"`
	RejectedFileCount           int64  `json:"rejectedFileCount"`
	UnresolvedRejectedFileCount int64  `json:"unresolvedRejectedFileCount"`
	AlreadyImportedItemCount    int64  `json:"alreadyImportedItemCount"`
	AlreadyImportedFileCount    int64  `json:"alreadyImportedFileCount"`
	Version                     int64  `json:"version"`
	CreatedAtMS                 int64  `json:"createdAtMs"`
	UpdatedAtMS                 int64  `json:"updatedAtMs"`
}

func queryImportList(
	ctx context.Context,
	database *sql.DB,
	arguments []any,
	capacity int,
) ([]importListItem, error) {
	rows, err := database.QueryContext(ctx, importListSQL, arguments...)
	if err != nil {
		return nil, fmt.Errorf("httpapi: query imports: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]importListItem, 0, capacity)
	for rows.Next() {
		var item importListItem
		var resolvedRejected int64
		if err := rows.Scan(
			&item.ID,
			&item.State,
			&item.PlatformInstanceName,
			&item.MetadataProvider,
			&item.ContentMode,
			&item.TotalItemCount,
			&item.ReviewPendingItemCount,
			&item.FailedItemCount,
			&item.RejectedFileCount,
			&resolvedRejected,
			&item.AlreadyImportedItemCount,
			&item.AlreadyImportedFileCount,
			&item.Version,
			&item.CreatedAtMS,
			&item.UpdatedAtMS,
		); err != nil {
			return nil, fmt.Errorf("httpapi: scan import: %w", err)
		}
		item.UnresolvedRejectedFileCount = item.RejectedFileCount - resolvedRejected
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("httpapi: scan imports: %w", err)
	}
	return items, nil
}

func (server *Server) encodeImportListCursor(
	filters importListFilters,
	items []importListItem,
) ([]importListItem, any, error) {
	if len(items) <= filters.limit {
		return items, nil, nil
	}
	last := items[filters.limit-1]
	items = items[:filters.limit]
	sortValue := last.UpdatedAtMS
	if filters.sortField == "createdAtMs" {
		sortValue = last.CreatedAtMS
	}
	token, err := server.cursors.Encode(cursor.Payload{
		OperationID:  "getAdminImports",
		FilterDigest: filters.digest,
		SortCode:     filters.sortCode,
		SortValues:   []string{strconv.FormatInt(sortValue, 10)},
		ID:           last.ID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("httpapi: encode import cursor: %w", err)
	}
	return items, token, nil
}

func (server *Server) imports(writer http.ResponseWriter, request *http.Request) {
	principal, _ := authn.PrincipalFromContext(request.Context())
	filters, err := parseImportListFilters(request.URL.Query(), principal.UserID)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "导入任务筛选无效", map[string]any{})
		return
	}
	arguments, err := server.importListArguments(filters)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	items, err := queryImportList(request.Context(), server.database, arguments, filters.limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	items, nextCursor, err := server.encodeImportListCursor(filters, items)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (server *Server) createImport(writer http.ResponseWriter, request *http.Request) {
	var body libraryimport.CreateRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "导入配置无效", map[string]any{})
		return
	}
	if body.TagIDs != nil {
		if _, err := tagging.ValidateIDs(body.TagIDs); err != nil {
			writeTagError(writer, request, err)
			return
		}
	}
	created, err := server.importer.Create(request.Context(), body)
	switch {
	case errors.Is(err, libraryimport.ErrMultiDiscModeUnavailable):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"MULTI_DISC_MODE_UNAVAILABLE", "目标目录不支持多盘导入", map[string]any{},
		)
		return
	case errors.Is(err, libraryimport.ErrMultiDiscPlaylistMissing):
		writeError(
			writer, request, http.StatusUnprocessableEntity,
			"MULTI_DISC_PLAYLIST_MISSING", "所选目录中没有 M3U 播放列表", map[string]any{},
		)
		return
	case errors.Is(err, tagging.ErrReferenceInvalid), errors.Is(err, tagging.ErrAssignmentLimitExceeded):
		writeTagError(writer, request, err)
		return
	case err != nil:
		writeError(writer, request, http.StatusConflict, "IMPORT_INPUT_INVALID", "上传或目标目录不可用于导入", map[string]any{})
		return
	}
	writeJSON(writer, http.StatusAccepted, created)
}
