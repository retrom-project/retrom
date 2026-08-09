package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/firmware"
)

func (server *Server) biosEntries(writer http.ResponseWriter, request *http.Request) {
	result, err := server.firmware.InspectArchive(request.Context(), request.PathValue("requirementId"))
	if err != nil {
		if errors.Is(err, firmware.ErrArchiveFactsNotFound) {
			writeError(
				writer,
				request,
				http.StatusNotFound,
				"BIOS_ARCHIVE_FACTS_NOT_FOUND",
				"当前 BIOS 没有可对比的归档条目信息",
				map[string]any{},
			)
			return
		}
		server.databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) installBIOS(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body firmware.InstallRequest
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "BIOS 安装请求无效", map[string]any{})
		return
	}
	result, err := server.firmware.Install(request.Context(), request.PathValue("requirementId"), version, body)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusUnprocessableEntity,
			"BIOS_INSTALLATION_INVALID",
			"上传文件、需求版本或 BIOS 内容无效",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.ValidatedRequirementVersion))
	writeJSON(writer, http.StatusCreated, result)
}
