package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/emulationstationimport"
)

func (server *Server) createEmulationStationImport(writer http.ResponseWriter, request *http.Request) {
	createFormatImport(
		writer,
		request,
		emulationstationimport.CreateRequest{},
		"EmulationStation 扫描配置无效",
		"/api/v1/admin/emulationstation-imports/",
		server.emulationStationImports.Create,
		func(summary emulationstationimport.Summary) string { return summary.ID },
		writeEmulationStationSummary,
		server.writeEmulationStationImportError,
	)
}

func (server *Server) emulationStationImportDetail(writer http.ResponseWriter, request *http.Request) {
	summary, err := server.emulationStationImports.Get(
		request.Context(), request.PathValue("emulationStationImportId"),
	)
	if err != nil {
		server.writeEmulationStationImportError(writer, request, err)
		return
	}
	writeEmulationStationSummary(writer, http.StatusOK, summary)
}

func (server *Server) updateEmulationStationMappings(writer http.ResponseWriter, request *http.Request) {
	updateFormatImportMappings(
		writer,
		request,
		"emulationStationImportId",
		"系统映射无效",
		"跳过的系统不能关联标签",
		func(mapping emulationstationimport.Mapping) serverImportMappingFields {
			return serverImportMappingFields{action: mapping.Action, tagIDs: mapping.TagIDs}
		},
		server.emulationStationImports.UpdateMappings,
		writeEmulationStationSummary,
		server.writeEmulationStationImportError,
	)
}

func (server *Server) startEmulationStationImport(writer http.ResponseWriter, request *http.Request) {
	startFormatImport(
		writer,
		request,
		"emulationStationImportId",
		server.emulationStationImports.StartImport,
		writeEmulationStationSummary,
		server.writeEmulationStationImportError,
	)
}

func (server *Server) cancelEmulationStationImport(writer http.ResponseWriter, request *http.Request) {
	cancelFormatImport(
		writer,
		request,
		"emulationStationImportId",
		server.emulationStationImports.Cancel,
		writeEmulationStationSummary,
		server.writeEmulationStationImportError,
	)
}

func (server *Server) retryEmulationStationImport(writer http.ResponseWriter, request *http.Request) {
	retryFormatImport(
		writer,
		request,
		"emulationStationImportId",
		server.emulationStationImports.Retry,
		writeEmulationStationSummary,
		server.writeEmulationStationImportError,
	)
}

func (server *Server) deleteEmulationStationImport(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer, request, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED",
			"需要当前计划版本", map[string]any{},
		)
		return
	}
	if err := server.emulationStationImports.Delete(
		request.Context(), request.PathValue("emulationStationImportId"), version,
	); err != nil {
		server.writeEmulationStationImportError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func writeEmulationStationSummary(
	writer http.ResponseWriter,
	status int,
	summary emulationstationimport.Summary,
) {
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, summary.Version))
	writeJSON(writer, status, summary)
}

func (server *Server) writeEmulationStationImportError(
	writer http.ResponseWriter,
	request *http.Request,
	err error,
) {
	server.writeFormatImportError(
		writer,
		request,
		err,
		emulationstationimport.ErrNotFound,
		"EmulationStation 导入请求当前不可执行",
		emulationStationImportErrorCode,
	)
}

func emulationStationImportErrorCode(err error) string {
	for _, sentinel := range []error{
		emulationstationimport.ErrInvalid,
		emulationstationimport.ErrGamelistAbsent,
		emulationstationimport.ErrNoValidGamelist,
		emulationstationimport.ErrScanLimit,
		emulationstationimport.ErrMapping,
		emulationstationimport.ErrVersionConflict,
		emulationstationimport.ErrNoSelection,
		emulationstationimport.ErrSourceChanged,
		emulationstationimport.ErrMappingTargetChanged,
		emulationstationimport.ErrExpired,
		emulationstationimport.ErrActive,
		emulationstationimport.ErrNotCancellable,
		emulationstationimport.ErrNotRetryable,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return ""
}
