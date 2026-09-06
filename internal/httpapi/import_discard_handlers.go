package httpapi

import (
	"database/sql"
	"errors"
	"net/http"

	"retrom/internal/authn"
	"retrom/internal/importdiscard"
)

func (server *Server) getImportBatchDiscard(writer http.ResponseWriter, request *http.Request) {
	status, err := server.importDiscards.Get(request.Context(), request.PathValue("kind"), request.PathValue("importId"))
	if err != nil {
		writeImportDiscardError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) discardImportBatch(writer http.ResponseWriter, request *http.Request) {
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct{}
	if err := decodeJSON(writer, request, &body, 1024); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "丢弃请求无效", map[string]any{})
		return
	}
	principal, _ := authn.PrincipalFromContext(request.Context())
	status, err := server.importDiscards.Request(
		request.Context(), request.PathValue("kind"), request.PathValue("importId"), principal.UserID,
	)
	if err != nil {
		writeImportDiscardError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, status)
}

func writeImportDiscardError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		serverNotFound(writer, request)
	case errors.Is(err, importdiscard.ErrInvalid):
		writeError(writer, request, http.StatusConflict, "IMPORT_BATCH_DISCARD_INVALID", "当前批次没有可丢弃的未处置内容", map[string]any{})
	default:
		writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "批次丢弃暂时不可用", map[string]any{})
	}
}
