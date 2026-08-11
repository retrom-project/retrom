package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cursor"
	"retrom/internal/serverimport"
)

func (server *Server) serverImportRoots(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"items": server.serverImports.Roots()})
}

//nolint:lll // Directory keyset cursor fields mirror the stable name/path ordering contract.
func (server *Server) serverImportDirectories(writer http.ResponseWriter, request *http.Request) {
	rootID := request.PathValue("rootId")
	path := request.URL.Query().Get("path")
	limit := 100
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"rootId": rootID, "path": path})
	afterName, afterPath := "", ""
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminServerImportRootDirectories", filter, "SERVER_DIRECTORY_ASC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterName, afterPath = payload.SortValues[0], payload.ID
	}
	directories, err := server.serverImports.Directories(rootID, path)
	if err != nil {
		server.writeServerImportError(writer, request, err)
		return
	}
	start := 0
	for start < len(directories) && (directories[start].Name < afterName || directories[start].Name == afterName && directories[start].RelativePath <= afterPath) {
		start++
	}
	end := min(start+limit, len(directories))
	items := directories[start:end]
	var next *string
	if end < len(directories) && len(items) > 0 {
		last := items[len(items)-1]
		token, encodeErr := server.cursors.Encode(cursor.Payload{OperationID: "getAdminServerImportRootDirectories", FilterDigest: filter, SortCode: "SERVER_DIRECTORY_ASC", SortValues: []string{last.Name}, ID: last.RelativePath})
		if encodeErr != nil {
			server.databaseError(writer, request, encodeErr)
			return
		}
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"rootId": rootID, "path": path, "items": items, "nextCursor": next})
}

func (server *Server) createServerImport(writer http.ResponseWriter, request *http.Request) {
	var body serverimport.CreateRequest
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "服务器导入配置无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	created, err := server.serverImports.Create(request.Context(), body, principal.UserID)
	if err != nil {
		server.writeServerImportError(writer, request, err)
		return
	}
	writer.Header().Set("Location", "/api/v1/admin/server-imports/"+created.ID)
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, created.Version))
	writeJSON(writer, http.StatusAccepted, created)
}

//nolint:lll // Import history keyset cursor fields mirror the created-at/ID ordering contract.
func (server *Server) serverImportList(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	kind := values.Get("kind")
	if kind != "" && kind != "BIOS_DIRECTORY" {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "服务器导入类型无效", map[string]any{})
		return
	}
	state := values.Get("state")
	if state != "" && !validServerImportState(state) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "服务器导入状态无效", map[string]any{})
		return
	}
	limit := 20
	if value := values.Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"kind": "BIOS_DIRECTORY", "state": state})
	var beforeAt int64
	beforeID := ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminServerImports", filter, "SERVER_IMPORT_CREATED_DESC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		beforeAt, err = strconv.ParseInt(payload.SortValues[0], 10, 64)
		if err != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		beforeID = payload.ID
	}
	items, err := server.serverImports.List(request.Context(), state, beforeAt, beforeID, limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(cursor.Payload{OperationID: "getAdminServerImports", FilterDigest: filter, SortCode: "SERVER_IMPORT_CREATED_DESC", SortValues: []string{strconv.FormatInt(last.CreatedAtMS, 10)}, ID: last.ID})
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

//nolint:lll // Item filters and cursor fields remain visibly aligned with the application query contract.
func (server *Server) serverImportDetail(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("serverImportId")
	summary, err := server.serverImports.Get(request.Context(), importID)
	if err != nil {
		server.writeServerImportError(writer, request, err)
		return
	}
	values := request.URL.Query()
	outcome, method := values.Get("outcome"), values.Get("matchMethod")
	limit := 50
	if value := values.Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"id": importID, "q": strings.TrimSpace(values.Get("q")), "outcome": outcome, "matchMethod": method})
	afterCore, afterName, afterID := "", "", ""
	if token := values.Get("cursor"); token != "" {
		payload, decodeErr := server.cursors.Decode(token, "getAdminServerImport", filter, "SERVER_IMPORT_ITEM_ASC")
		if decodeErr != nil || len(payload.SortValues) != 2 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterCore, afterName, afterID = payload.SortValues[0], payload.SortValues[1], payload.ID
	}
	items, err := server.serverImports.Items(request.Context(), importID, strings.TrimSpace(values.Get("q")), outcome, method, afterCore, afterName, afterID, limit+1)
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(cursor.Payload{OperationID: "getAdminServerImport", FilterDigest: filter, SortCode: "SERVER_IMPORT_ITEM_ASC", SortValues: []string{last.CoreName, last.LogicalName}, ID: last.RequirementID})
		next = &token
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, summary.Version))
	writeJSON(writer, http.StatusOK, map[string]any{"summary": summary, "items": items, "nextCursor": next})
}

//nolint:lll // Candidate cursor fields mirror the rank/ID ordering contract.
func (server *Server) serverImportCandidates(writer http.ResponseWriter, request *http.Request) {
	importID, requirementID := request.PathValue("serverImportId"), request.PathValue("requirementId")
	limit := 50
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"id": importID, "requirementId": requirementID})
	var rank int64
	afterID := ""
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminServerImportBIOSCandidates", filter, "SERVER_CANDIDATE_ASC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		rank, err = strconv.ParseInt(payload.SortValues[0], 10, 64)
		if err != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterID = payload.ID
	}
	items, err := server.serverImports.Candidates(request.Context(), importID, requirementID, rank, afterID, limit+1)
	if err != nil {
		server.writeServerImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		value := int64(9223372036854775807)
		if last.RankOrdinal != nil {
			value = *last.RankOrdinal
		}
		token, _ := server.cursors.Encode(cursor.Payload{OperationID: "getAdminServerImportBIOSCandidates", FilterDigest: filter, SortCode: "SERVER_CANDIDATE_ASC", SortValues: []string{strconv.FormatInt(value, 10)}, ID: last.ID})
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

type serverImportCancelBody struct {
	Reason string `json:"reason"`
}

//nolint:lll // The domain cancellation call carries the complete optimistic-locking request.
func (server *Server) cancelServerImport(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前任务版本", map[string]any{})
		return
	}
	var body serverImportCancelBody
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "取消原因无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	summary, pending, err := server.serverImports.Cancel(request.Context(), request.PathValue("serverImportId"), version, body.Reason, principal.UserID)
	if err != nil {
		server.writeServerImportError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, summary.Version))
	status := http.StatusOK
	if pending {
		status = http.StatusAccepted
	}
	writeJSON(writer, status, summary)
}

//nolint:lll // The domain retry call carries the complete optimistic-locking request.
func (server *Server) retryServerImport(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "需要当前任务版本", map[string]any{})
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "重试请求无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	summary, err := server.serverImports.Retry(request.Context(), request.PathValue("serverImportId"), version, principal.UserID)
	if err != nil {
		server.writeServerImportError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, summary.Version))
	writeJSON(writer, http.StatusAccepted, summary)
}

func validServerImportState(value string) bool {
	switch value {
	case "QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE", "CANCEL_REQUESTED", "CANCELLED", "FAILED":
		return true
	}
	return false
}

func (server *Server) writeServerImportError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, serverimport.ErrRootIDInvalid):
		writeError(writer, request, http.StatusBadRequest, "SERVER_IMPORT_ROOT_ID_INVALID", "服务器位置标识无效", map[string]any{})
	case errors.Is(err, serverimport.ErrPathInvalid):
		writeError(writer, request, http.StatusBadRequest, "SERVER_IMPORT_PATH_INVALID", "服务器目录无效", map[string]any{})
	case errors.Is(err, serverimport.ErrRootNotFound):
		writeError(writer, request, http.StatusNotFound, "SERVER_IMPORT_ROOT_NOT_FOUND", "服务器导入资源不存在", map[string]any{})
	case errors.Is(err, serverimport.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(writer, request, http.StatusNotFound, "RESOURCE_NOT_FOUND", "请求的资源不存在", map[string]any{})
	case errors.Is(err, serverimport.ErrRootUnavailable):
		writeError(writer, request, http.StatusConflict, "SERVER_IMPORT_ROOT_UNAVAILABLE", "服务器位置当前不可用", map[string]any{})
	case errors.Is(err, serverimport.ErrActive):
		writeError(writer, request, http.StatusConflict, "SERVER_BIOS_IMPORT_ACTIVE", "已有 BIOS 服务器导入正在运行", map[string]any{})
	case errors.Is(err, serverimport.ErrCatalogEmpty):
		writeError(writer, request, http.StatusConflict, "BIOS_CATALOG_EMPTY", "当前 BIOS 目录为空", map[string]any{})
	case errors.Is(err, serverimport.ErrCatalogInvalid):
		writeError(writer, request, http.StatusConflict, "BIOS_CATALOG_INVALID", "BIOS 目录证据无效", map[string]any{})
	case errors.Is(err, serverimport.ErrNotCancellable):
		writeError(writer, request, http.StatusConflict, "SERVER_IMPORT_NOT_CANCELLABLE", "任务当前不可取消", map[string]any{})
	case errors.Is(err, serverimport.ErrNotRetryable):
		writeError(writer, request, http.StatusConflict, "SERVER_IMPORT_NOT_RETRYABLE", "任务当前不可重试", map[string]any{})
	default:
		server.databaseError(writer, request, err)
	}
}
