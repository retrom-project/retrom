package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/cursor"
	"retrom/internal/pegasusimport"
)

func (server *Server) createPegasusImport(writer http.ResponseWriter, request *http.Request) {
	createFormatImport(
		writer,
		request,
		pegasusimport.CreateRequest{},
		"Pegasus 扫描配置无效",
		"/api/v1/admin/pegasus-imports/",
		server.pegasusImports.Create,
		func(summary pegasusimport.Summary) string { return summary.ID },
		writePegasusSummary,
		server.writePegasusImportError,
	)
}

func (server *Server) pegasusImportList(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	state := values.Get("state")
	if state != "" && !validPegasusState(state) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "Pegasus 导入状态无效", map[string]any{})
		return
	}
	limit := 20
	if value := values.Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"state": state})
	var beforeAt int64
	beforeID := ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminPegasusImports", filter, "PEGASUS_IMPORT_CREATED_DESC")
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
	items, err := server.pegasusImports.List(request.Context(), state, beforeAt, beforeID, limit+1)
	if err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminPegasusImports",
				FilterDigest: filter,
				SortCode:     "PEGASUS_IMPORT_CREATED_DESC",
				SortValues:   []string{strconv.FormatInt(last.CreatedAtMS, 10)},
				ID:           last.ID,
			},
		)
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) pegasusImportDetail(writer http.ResponseWriter, request *http.Request) {
	summary, err := server.pegasusImports.Get(request.Context(), request.PathValue("pegasusImportId"))
	if err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	writePegasusSummary(writer, http.StatusOK, summary)
}

func (server *Server) pegasusImportCollections(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("pegasusImportId")
	limit := 100
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filter := cursor.FilterDigest(map[string]any{"id": importID})
	afterPath, afterID := "", ""
	var afterOrdinal int64
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(
			token,
			"getAdminPegasusImportCollections",
			filter,
			"PEGASUS_COLLECTION_ASC",
		)
		if err != nil || len(payload.SortValues) != 2 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterPath = payload.SortValues[0]
		afterOrdinal, err = strconv.ParseInt(payload.SortValues[1], 10, 64)
		if err != nil {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterID = payload.ID
	}
	items, err := server.pegasusImports.Collections(
		request.Context(),
		importID,
		afterPath,
		afterOrdinal,
		afterID,
		limit+1,
	)
	if err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminPegasusImportCollections",
				FilterDigest: filter,
				SortCode:     "PEGASUS_COLLECTION_ASC",
				SortValues:   []string{last.MetadataRelativePath, strconv.FormatInt(last.SegmentOrdinal, 10)},
				ID:           last.ID,
			},
		)
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) updatePegasusMappings(writer http.ResponseWriter, request *http.Request) {
	updateFormatImportMappings(
		writer,
		request,
		"pegasusImportId",
		"集合映射无效",
		"跳过的集合不能关联标签",
		func(mapping pegasusimport.Mapping) serverImportMappingFields {
			return serverImportMappingFields{action: mapping.Action, tagIDs: mapping.TagIDs}
		},
		server.pegasusImports.UpdateMappings,
		writePegasusSummary,
		server.writePegasusImportError,
	)
}

func (server *Server) startPegasusImport(writer http.ResponseWriter, request *http.Request) {
	startFormatImport(
		writer,
		request,
		"pegasusImportId",
		server.pegasusImports.StartImport,
		writePegasusSummary,
		server.writePegasusImportError,
	)
}

func (server *Server) pegasusImportItems(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("pegasusImportId")
	values := request.URL.Query()
	limit := 50
	if value := values.Get("limit"); value != "" {
		limit, _ = strconv.Atoi(value)
	}
	filters := map[string]any{
		"id":           importID,
		"q":            strings.TrimSpace(values.Get("q")),
		"outcome":      values.Get("outcome"),
		"warning":      values.Get("warning"),
		"collectionId": values.Get("collectionId"),
	}
	filter := cursor.FilterDigest(filters)
	afterTitle, afterID := "", ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.cursors.Decode(token, "getAdminPegasusImportItems", filter, "PEGASUS_ITEM_ASC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterTitle, afterID = payload.SortValues[0], payload.ID
	}
	items, err := server.pegasusImports.Items(
		request.Context(),
		importID,
		strings.TrimSpace(values.Get("q")),
		values.Get("outcome"),
		values.Get("warning"),
		values.Get("collectionId"),
		afterTitle,
		afterID,
		limit+1,
	)
	if err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminPegasusImportItems",
				FilterDigest: filter,
				SortCode:     "PEGASUS_ITEM_ASC",
				SortValues:   []string{last.Title},
				ID:           last.ID,
			},
		)
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) cancelPegasusImport(writer http.ResponseWriter, request *http.Request) {
	cancelFormatImport(
		writer,
		request,
		"pegasusImportId",
		server.pegasusImports.Cancel,
		writePegasusSummary,
		server.writePegasusImportError,
	)
}

func (server *Server) retryPegasusImport(writer http.ResponseWriter, request *http.Request) {
	retryFormatImport(
		writer,
		request,
		"pegasusImportId",
		server.pegasusImports.Retry,
		writePegasusSummary,
		server.writePegasusImportError,
	)
}

func (server *Server) deletePegasusImport(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前计划版本",
			map[string]any{},
		)
		return
	}
	if err := server.pegasusImports.Delete(request.Context(), request.PathValue("pegasusImportId"), version); err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func writePegasusSummary(writer http.ResponseWriter, status int, summary pegasusimport.Summary) {
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, summary.Version))
	writeJSON(writer, status, summary)
}

func validPegasusState(value string) bool {
	switch value {
	case "SCANNING",
		"AWAITING_MAPPING",
		"QUEUED",
		"RUNNING",
		"PARTIAL_FAILURE",
		"COMPLETED",
		"CANCEL_REQUESTED",
		"CANCELLED",
		"FAILED",
		"EXPIRED":
		return true
	}
	return false
}

func (server *Server) writePegasusImportError(writer http.ResponseWriter, request *http.Request, err error) {
	server.writeFormatImportError(
		writer,
		request,
		err,
		pegasusimport.ErrNotFound,
		"Pegasus 导入请求当前不可执行",
		pegasusImportErrorCode,
	)
}

func pegasusImportErrorCode(err error) string {
	for _, sentinel := range []error{
		pegasusimport.ErrInvalid,
		pegasusimport.ErrMetadataAbsent,
		pegasusimport.ErrScanLimit,
		pegasusimport.ErrMapping,
		pegasusimport.ErrVersionConflict,
		pegasusimport.ErrNoSelection,
		pegasusimport.ErrSourceChanged,
		pegasusimport.ErrExpired,
		pegasusimport.ErrActive,
		pegasusimport.ErrNotCancellable,
		pegasusimport.ErrNotRetryable,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return ""
}
