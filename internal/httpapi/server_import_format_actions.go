package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/serversource"
	"retrom/internal/tagging"
)

type serverImportMappingFields struct {
	action string
	tagIDs []string
}

func createFormatImport[Request, Summary any](
	writer http.ResponseWriter,
	request *http.Request,
	body Request,
	invalidMessage, locationPrefix string,
	create func(context.Context, Request, string) (Summary, error),
	identity func(Summary) string,
	writeSummary func(http.ResponseWriter, int, Summary),
	writeDomainError func(http.ResponseWriter, *http.Request, error),
) {
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", invalidMessage, map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	created, err := create(request.Context(), body, principal.UserID)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.Header().Set("Location", locationPrefix+identity(created))
	writeSummary(writer, http.StatusAccepted, created)
}

func updateFormatImportMappings[Mapping, Summary any](
	writer http.ResponseWriter,
	request *http.Request,
	pathParameter, invalidMessage, skippedTagMessage string,
	fields func(Mapping) serverImportMappingFields,
	update func(context.Context, string, int64, []Mapping) (Summary, error),
	writeSummary func(http.ResponseWriter, int, Summary),
	writeDomainError func(http.ResponseWriter, *http.Request, error),
) {
	version, ok := requireFormatImportVersion(writer, request, "需要当前计划版本")
	if !ok {
		return
	}
	body := struct {
		Mappings []Mapping `json:"mappings"`
	}{}
	if err := decodeJSON(writer, request, &body, 128<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", invalidMessage, map[string]any{})
		return
	}
	for _, mapping := range body.Mappings {
		values := fields(mapping)
		if _, err := tagging.ValidateIDs(values.tagIDs); err != nil {
			writeTagError(writer, request, err)
			return
		}
		if values.action == "SKIP" && len(values.tagIDs) != 0 {
			writeError(
				writer, request, http.StatusBadRequest, "INVALID_REQUEST", skippedTagMessage, map[string]any{},
			)
			return
		}
	}
	summary, err := update(request.Context(), request.PathValue(pathParameter), version, body.Mappings)
	if err != nil {
		if errors.Is(err, tagging.ErrReferenceInvalid) || errors.Is(err, tagging.ErrAssignmentLimitExceeded) {
			writeTagError(writer, request, err)
			return
		}
		writeDomainError(writer, request, err)
		return
	}
	writeSummary(writer, http.StatusOK, summary)
}

func startFormatImport[Summary any](
	writer http.ResponseWriter,
	request *http.Request,
	pathParameter string,
	start func(context.Context, string, int64) (Summary, error),
	writeSummary func(http.ResponseWriter, int, Summary),
	writeDomainError func(http.ResponseWriter, *http.Request, error),
) {
	version, ok := requireFormatImportVersion(writer, request, "需要当前计划版本")
	if !ok {
		return
	}
	body := struct {
		Version int64 `json:"version"`
	}{}
	if err := decodeJSON(writer, request, &body, 4096); err != nil || body.Version != version {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "启动版本无效", map[string]any{})
		return
	}
	summary, err := start(request.Context(), request.PathValue(pathParameter), version)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeSummary(writer, http.StatusAccepted, summary)
}

func cancelFormatImport[Summary any](
	writer http.ResponseWriter,
	request *http.Request,
	pathParameter string,
	cancel func(context.Context, string, int64, string, string) (Summary, bool, error),
	writeSummary func(http.ResponseWriter, int, Summary),
	writeDomainError func(http.ResponseWriter, *http.Request, error),
) {
	version, ok := requireFormatImportVersion(writer, request, "需要当前任务版本")
	if !ok {
		return
	}
	body := struct {
		Reason string `json:"reason"`
	}{}
	if err := decodeJSON(writer, request, &body, 64<<10); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "取消原因无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	summary, pending, err := cancel(
		request.Context(), request.PathValue(pathParameter), version, body.Reason, principal.UserID,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	status := http.StatusOK
	if pending {
		status = http.StatusAccepted
	}
	writeSummary(writer, status, summary)
}

func retryFormatImport[Summary any](
	writer http.ResponseWriter,
	request *http.Request,
	pathParameter string,
	retry func(context.Context, string, int64, string) (Summary, error),
	writeSummary func(http.ResponseWriter, int, Summary),
	writeDomainError func(http.ResponseWriter, *http.Request, error),
) {
	version, ok := requireFormatImportVersion(writer, request, "需要当前任务版本")
	if !ok {
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 4096); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "重试请求无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	summary, err := retry(request.Context(), request.PathValue(pathParameter), version, principal.UserID)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeSummary(writer, http.StatusAccepted, summary)
}

func requireFormatImportVersion(
	writer http.ResponseWriter,
	request *http.Request,
	message string,
) (int64, bool) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", message, map[string]any{},
		)
		return 0, false
	}
	return version, true
}

func (server *Server) writeFormatImportError(
	writer http.ResponseWriter,
	request *http.Request,
	err, notFound error,
	domainMessage string,
	domainCode func(error) string,
) {
	status := http.StatusConflict
	var code, message string
	switch {
	case errors.Is(err, notFound), errors.Is(err, sql.ErrNoRows):
		status, code, message = http.StatusNotFound, "RESOURCE_NOT_FOUND", "请求的资源不存在"
	case errors.Is(err, serversource.ErrRootIDInvalid):
		status, code, message = http.StatusBadRequest, "SERVER_IMPORT_ROOT_ID_INVALID", "服务器位置标识无效"
	case errors.Is(err, serversource.ErrPathInvalid):
		status, code, message = http.StatusBadRequest, "SERVER_IMPORT_PATH_INVALID", "服务器目录无效"
	case errors.Is(err, serversource.ErrRootNotFound):
		status, code, message = http.StatusNotFound, "SERVER_IMPORT_ROOT_NOT_FOUND", "服务器导入资源不存在"
	case errors.Is(err, serversource.ErrRootUnavailable):
		code, message = "SERVER_IMPORT_ROOT_UNAVAILABLE", "服务器位置当前不可用"
	default:
		code = domainCode(err)
		if code == "" {
			server.databaseError(writer, request, err)
			return
		}
		message = domainMessage
	}
	writeError(writer, request, status, code, message, map[string]any{})
}
