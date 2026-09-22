package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"retrom/internal/authn"

	"retrom/internal/cursor"
	"retrom/internal/service/sourceimport"
)

func (server *Server) createSourceImport(writer http.ResponseWriter, request *http.Request) {
	createFormatImport(
		writer,
		request,
		sourceimport.CreateRequest{},
		"Source 扫描配置无效",
		"/api/v1/admin/source-imports/",
		server.sourceImports.Create,
		func(summary sourceimport.Summary) string { return summary.ID },
		writeSourceSummary,
		server.writeSourceImportError,
	)
}

func (server *Server) sourceImportList(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	state := values.Get("state")
	if state != "" && !validSourceState(state) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_QUERY", "Source 导入状态无效", map[string]any{})
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
		payload, err := server.cursors.Decode(token, "getAdminSourceImports", filter, "SOURCE_IMPORT_CREATED_DESC")
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
	items, err := server.sourceImports.List(request.Context(), sourceimport.ListQuery{
		State: state, BeforeAtMS: beforeAt, BeforeID: beforeID, Limit: limit + 1,
	})
	if err != nil {
		server.writeSourceImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminSourceImports",
				FilterDigest: filter,
				SortCode:     "SOURCE_IMPORT_CREATED_DESC",
				SortValues:   []string{strconv.FormatInt(last.CreatedAtMS, 10)},
				ID:           last.ID,
			},
		)
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) sourceImportDetail(writer http.ResponseWriter, request *http.Request) {
	summary, err := server.sourceImports.Get(request.Context(), request.PathValue("sourceImportId"))
	if err != nil {
		server.writeSourceImportError(writer, request, err)
		return
	}
	writeSourceSummary(writer, http.StatusOK, summary)
}

func (server *Server) sourceImportCollections(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("sourceImportId")
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
			"getAdminSourceImportCollections",
			filter,
			"SOURCE_COLLECTION_ASC",
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
	items, err := server.sourceImports.Collections(
		request.Context(), sourceimport.CollectionQuery{
			ImportID: importID, AfterPath: afterPath, AfterOrdinal: afterOrdinal, AfterID: afterID, Limit: limit + 1,
		},
	)
	if err != nil {
		server.writeSourceImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminSourceImportCollections",
				FilterDigest: filter,
				SortCode:     "SOURCE_COLLECTION_ASC",
				SortValues:   []string{last.MetadataRelativePath, strconv.FormatInt(last.SegmentOrdinal, 10)},
				ID:           last.ID,
			},
		)
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) updateSourceMappings(writer http.ResponseWriter, request *http.Request) {
	updateFormatImportMappings(
		writer,
		request,
		"sourceImportId",
		"集合映射无效",
		"跳过的集合不能关联标签",
		func(mapping sourceimport.Mapping) serverImportMappingFields {
			return serverImportMappingFields{action: mapping.Action, tagIDs: mapping.TagIDs}
		},
		func(ctx context.Context, id string, version int64, mappings []sourceimport.Mapping) (sourceimport.Summary, error) {
			principal, _ := authn.PrincipalFromContext(ctx)
			return server.sourceImports.UpdateMappings(ctx, id, version, mappings, principal.UserID)
		},
		writeSourceSummary,
		server.writeSourceImportError,
	)
}

func (server *Server) startSourceImport(writer http.ResponseWriter, request *http.Request) {
	startFormatImport(
		writer,
		request,
		"sourceImportId",
		func(ctx context.Context, id string, version int64) (sourceimport.Summary, error) {
			principal, _ := authn.PrincipalFromContext(ctx)
			return server.sourceImports.StartImport(ctx, id, version, principal.UserID)
		},
		writeSourceSummary,
		server.writeSourceImportError,
	)
}

func (server *Server) sourceImportItems(writer http.ResponseWriter, request *http.Request) {
	importID := request.PathValue("sourceImportId")
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
		payload, err := server.cursors.Decode(token, "getAdminSourceImportItems", filter, "SOURCE_ITEM_ASC")
		if err != nil || len(payload.SortValues) != 1 {
			writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "分页游标无效", map[string]any{})
			return
		}
		afterTitle, afterID = payload.SortValues[0], payload.ID
	}
	items, err := server.sourceImports.Items(
		request.Context(), sourceimport.ItemQuery{
			ImportID: importID, Text: strings.TrimSpace(values.Get("q")), Outcome: values.Get("outcome"),
			Warning: values.Get("warning"), CollectionID: values.Get("collectionId"),
			AfterTitle: afterTitle, AfterID: afterID, Limit: limit + 1,
		},
	)
	if err != nil {
		server.writeSourceImportError(writer, request, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		token, _ := server.cursors.Encode(
			cursor.Payload{
				OperationID:  "getAdminSourceImportItems",
				FilterDigest: filter,
				SortCode:     "SOURCE_ITEM_ASC",
				SortValues:   []string{last.Title},
				ID:           last.ID,
			},
		)
		next = &token
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (server *Server) cancelSourceImport(writer http.ResponseWriter, request *http.Request) {
	cancelFormatImport(
		writer,
		request,
		"sourceImportId",
		server.sourceImports.Cancel,
		writeSourceSummary,
		server.writeSourceImportError,
	)
}

func (server *Server) retrySourceImport(writer http.ResponseWriter, request *http.Request) {
	retryFormatImport(
		writer,
		request,
		"sourceImportId",
		server.sourceImports.Retry,
		writeSourceSummary,
		server.writeSourceImportError,
	)
}

func (server *Server) deleteSourceImport(writer http.ResponseWriter, request *http.Request) {
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
	if err := server.sourceImports.Delete(
		request.Context(), request.PathValue("sourceImportId"), version, principal.UserID,
	); err != nil {
		server.writeSourceImportError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func writeSourceSummary(writer http.ResponseWriter, status int, summary sourceimport.Summary) {
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, summary.Version))
	writeJSON(writer, status, summary)
}

func validSourceState(value string) bool {
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

func (server *Server) writeSourceImportError(writer http.ResponseWriter, request *http.Request, err error) {
	server.writeFormatImportError(
		writer,
		request,
		err,
		sourceimport.ErrNotFound,
		"Source 导入请求当前不可执行",
		sourceImportErrorCode,
	)
}

func sourceImportErrorCode(err error) string {
	for _, sentinel := range []error{
		sourceimport.ErrInvalid,
		sourceimport.ErrMetadataAbsent,
		sourceimport.ErrScanLimit,
		sourceimport.ErrMapping,
		sourceimport.ErrVersionConflict,
		sourceimport.ErrNoSelection,
		sourceimport.ErrSourceChanged,
		sourceimport.ErrExpired,
		sourceimport.ErrActive,
		sourceimport.ErrNotCancellable,
		sourceimport.ErrNotRetryable,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return ""
}
