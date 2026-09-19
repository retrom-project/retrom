package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/capability/security/authn"
	pegasusimportmodel "retrom/internal/model/pegasusimport"

	"retrom/internal/foundation/cursor"
)

func (server *Server) createPegasusImport(writer http.ResponseWriter, request *http.Request) {
	createFormatImport(
		writer,
		request, pegasusimportmodel.CreateRequest{}, "Pegasus 扫描配置无效",
		"/api/v1/admin/pegasus-imports/",
		server.pegasusImports.Create,
		func(summary pegasusimportmodel.Summary) string { return summary.ID },
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
	filter := cursorFilterDigest(map[string]any{"state": state})
	var beforeAt int64
	beforeID := ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.decodeCursor(token, "getAdminPegasusImports", filter, "PEGASUS_IMPORT_CREATED_DESC")
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
	items, err := server.pegasusImports.List(request.Context(), pegasusimportmodel.ListQuery{
		State: state, BeforeAtMS: beforeAt, BeforeID: beforeID, Limit: limit + 1,
	})
	if err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.encodeCursor(
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
	filter := cursorFilterDigest(map[string]any{"id": importID})
	afterPath, afterID := "", ""
	var afterOrdinal int64
	if token := request.URL.Query().Get("cursor"); token != "" {
		payload, err := server.decodeCursor(
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
		request.Context(), pegasusimportmodel.CollectionQuery{
			ImportID: importID, AfterPath: afterPath, AfterOrdinal: afterOrdinal, AfterID: afterID, Limit: limit + 1,
		},
	)
	if err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.encodeCursor(
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
		func(mapping pegasusimportmodel.Mapping) serverImportMappingFields {
			return serverImportMappingFields{action: mapping.Action, tagIDs: mapping.TagIDs}
		},
		func(ctx context.Context, id string, version int64, mappings []pegasusimportmodel.Mapping) (
			pegasusimportmodel.Summary,
			error,
		) {
			principal, _ := authn.PrincipalFromContext(ctx)
			return server.pegasusImports.UpdateMappings(ctx, id, version, mappings, principal.UserID)
		},
		writePegasusSummary,
		server.writePegasusImportError,
	)
}

func (server *Server) startPegasusImport(writer http.ResponseWriter, request *http.Request) {
	startFormatImport(
		writer,
		request,
		"pegasusImportId",
		func(ctx context.Context, id string, version int64) (pegasusimportmodel.Summary, error) {
			principal, _ := authn.PrincipalFromContext(ctx)
			return server.pegasusImports.StartImport(ctx, id, version, principal.UserID)
		},
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
	filter := cursorFilterDigest(filters)
	afterTitle, afterID := "", ""
	if token := values.Get("cursor"); token != "" {
		payload, err := server.decodeCursor(token, "getAdminPegasusImportItems", filter, "PEGASUS_ITEM_ASC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterTitle, afterID = payload.SortValues[0], payload.ID
	}
	items, err := server.pegasusImports.Items(
		request.Context(), pegasusimportmodel.ItemQuery{
			ImportID: importID, Text: strings.TrimSpace(values.Get("q")), Outcome: values.Get("outcome"),
			Warning: values.Get("warning"), CollectionID: values.Get("collectionId"),
			AfterTitle: afterTitle, AfterID: afterID, Limit: limit + 1,
		},
	)
	if err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.encodeCursor(
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
	principal, _ := authn.PrincipalFromContext(request.Context())
	if err := server.pegasusImports.Delete(
		request.Context(), request.PathValue("pegasusImportId"), version, principal.UserID,
	); err != nil {
		server.writePegasusImportError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func writePegasusSummary(writer http.ResponseWriter, status int, summary pegasusimportmodel.Summary) {
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
		err, pegasusimportmodel.ErrNotFound, "Pegasus 导入请求当前不可执行",
		pegasusImportErrorCode,
	)
}

func pegasusImportErrorCode(err error) string {
	for _, sentinel := range []error{
		pegasusimportmodel.ErrInvalid,
		pegasusimportmodel.ErrMetadataAbsent,
		pegasusimportmodel.ErrScanLimit,
		pegasusimportmodel.ErrMapping,
		pegasusimportmodel.ErrVersionConflict,
		pegasusimportmodel.ErrNoSelection,
		pegasusimportmodel.ErrSourceChanged,
		pegasusimportmodel.ErrExpired,
		pegasusimportmodel.ErrActive,
		pegasusimportmodel.ErrNotCancellable,
		pegasusimportmodel.ErrNotRetryable,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return ""
}
