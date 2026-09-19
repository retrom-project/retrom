package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"retrom/internal/adapter/integration/libraryimport"
	librarycomposition "retrom/internal/bootstrap/composition/libraryimport"
	"retrom/internal/capability/engine/rpgmaker/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	"retrom/internal/capability/format/importing"
	"retrom/internal/capability/security/authn"
	"retrom/internal/foundation/cursor"
	libraryimportmodel "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
	libraryservice "retrom/internal/service/libraryimport"
)

type importListItem = libraryimportmodel.ImportListItem

func (server *Server) importReads() *libraryservice.ImportReads {
	return librarycomposition.NewImportReads(server.database)
}

func (server *Server) importSummary(writer http.ResponseWriter, request *http.Request) {
	summary, err := server.importReads().Summary(request.Context())
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, summary)
}

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

func (server *Server) importListQuery(filters importListFilters) (libraryimportmodel.ImportListQuery, error) {
	cursorID := ""
	cursorValue := int64(0)
	if filters.cursorToken != "" {
		payload, err := server.cursors.Decode(
			filters.cursorToken, "getAdminImports", filters.digest, filters.sortCode,
		)
		if err != nil || len(payload.SortValues) != 1 {
			return libraryimportmodel.ImportListQuery{}, errInvalidCursorPayload
		}
		parsed, err := strconv.ParseInt(payload.SortValues[0], 10, 64)
		if err != nil {
			return libraryimportmodel.ImportListQuery{}, errInvalidCursorPayload
		}
		cursorID, cursorValue = payload.ID, parsed
	}
	return libraryimportmodel.ImportListQuery{
		QueryText: filters.queryText, State: filters.state, PlatformID: filters.platformID,
		SortCode: filters.sortCode, CursorID: cursorID, CursorValue: cursorValue,
		Limit: filters.limit + 1,
	}, nil
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
	query, err := server.importListQuery(filters)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
		return
	}
	items, err := server.importReads().List(request.Context(), query)
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
	var body libraryimportmodel.ImportRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "导入配置无效", map[string]any{})
		return
	}
	if body.TagIDs != nil {
		if _, err := taggingmodel.ValidateIDs(body.TagIDs); err != nil {
			writeTagError(writer, request, err)
			return
		}
	}
	created, err := server.importAdmissions.Queue(request.Context(), body)
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
	case errors.Is(err, taggingmodel.ErrReferenceInvalid), errors.Is(err, taggingmodel.ErrAssignmentLimitExceeded):
		writeTagError(writer, request, err)
		return
	case err != nil:
		status, code, message := importCreationError(err)
		if status == http.StatusInternalServerError {
			server.databaseError(writer, request, err)
			return
		}
		writeError(writer, request, status, code, message, map[string]any{})
		return
	}
	writeJSON(writer, http.StatusAccepted, created)
}

func importCreationError(err error) (int, string, string) {
	var detectionError *detector.Error
	if errors.As(err, &detectionError) {
		return detectorImportStatus(
				detectionError.Code,
			), string(
				detectionError.Code,
			), "RPG Maker 项目与所选版本不兼容"
	}
	var projectError *fileset.ProjectError
	if errors.As(err, &projectError) {
		status := http.StatusUnprocessableEntity
		switch projectError.Code {
		case fileset.CodeProjectNotFound:
			status = http.StatusBadRequest
		case fileset.CodeRootAmbiguous:
			status = http.StatusConflict
		case fileset.CodePathCollision:
			status = http.StatusUnprocessableEntity
		}
		return status, string(projectError.Code), "RPG Maker 项目结构无效"
	}
	switch {
	case errors.Is(err, importing.ErrArchiveLimitExceeded):
		return http.StatusRequestEntityTooLarge, "ARCHIVE_LIMIT_EXCEEDED", "项目归档超过安全限制"
	case errors.Is(err, importing.ErrArchiveEncrypted):
		return http.StatusUnprocessableEntity, "ARCHIVE_ENCRYPTED_UNSUPPORTED", "不支持加密项目归档"
	case errors.Is(err, importing.ErrArchiveVolumeUnsupported):
		return http.StatusUnprocessableEntity, "ARCHIVE_VOLUME_UNSUPPORTED", "不支持分卷项目归档"
	case errors.Is(err, importing.ErrArchiveCasefoldCollision):
		return http.StatusUnprocessableEntity, "RPG_PATH_COLLISION", "RPG Maker 项目路径发生冲突"
	case errors.Is(err, libraryimportmodel.ErrInvalid), errors.Is(err, libraryimportmodel.ErrVersionConflict):
		return http.StatusConflict, "IMPORT_INPUT_INVALID", "上传或目标目录不可用于导入"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "导入创建失败"
	}
}

func detectorImportStatus(code detector.Code) int {
	if code == detector.CodeProjectNotFound {
		return http.StatusBadRequest
	}
	if code == detector.CodeGenerationAmbiguous {
		return http.StatusConflict
	}
	return http.StatusUnprocessableEntity
}
