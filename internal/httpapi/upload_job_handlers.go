package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"retrom/internal/uploads"
)

func (server *Server) createUpload(writer http.ResponseWriter, request *http.Request) {
	var body uploads.CreateRequest
	if err := decodeJSON(writer, request, &body, 2<<20); err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "上传清单无效", map[string]any{})
		return
	}
	session, err := server.uploads.Create(request.Context(), body)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "上传清单无效", map[string]any{})
		return
	}
	writer.Header().Set("ETag", `"v1"`)
	writeJSON(writer, http.StatusCreated, session)
}

func (server *Server) getUpload(writer http.ResponseWriter, request *http.Request) {
	session, err := server.uploads.Get(request.Context(), request.PathValue("uploadId"))
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, session.Version))
	writeJSON(writer, http.StatusOK, session)
}

func (server *Server) putUploadPart(writer http.ResponseWriter, request *http.Request) {
	partNo, err := strconv.Atoi(request.PathValue("partNo"))
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "分块编号无效", map[string]any{})
		return
	}
	body := http.MaxBytesReader(writer, request.Body, uploads.PartSize+1)
	err = server.uploads.PutPart(
		request.Context(),
		request.PathValue("uploadId"),
		request.PathValue("fileId"),
		partNo,
		request.Header.Get("Content-Range"),
		request.Header.Get("Content-Digest"),
		body,
	)
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusBadRequest,
			"UPLOAD_RANGE_MISMATCH",
			"上传分块校验失败",
			map[string]any{"partNo": partNo},
		)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) completeUpload(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"缺少有效资源版本",
			map[string]any{},
		)
		return
	}
	jobID, finalization, err := server.uploads.Complete(request.Context(), request.PathValue("uploadId"), version)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "VERSION_CONFLICT", "上传状态或版本已经变化", map[string]any{})
		return
	}
	writeJSON(
		writer,
		http.StatusAccepted,
		map[string]any{
			"uploadId":       request.PathValue("uploadId"),
			"jobId":          jobID,
			"finalizationNo": finalization,
			"state":          "FINALIZING",
		},
	)
}

func (server *Server) cancelUpload(writer http.ResponseWriter, request *http.Request) {
	version, err := ParseETag(request.Header.Get("If-Match"))
	if err != nil {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前上传版本",
			map[string]any{},
		)
		return
	}
	result, pending, err := server.uploads.Cancel(request.Context(), request.PathValue("uploadId"), version)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "UPLOAD_CANCEL_CONFLICT", "上传状态或版本已经变化", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	if pending {
		writeJSON(writer, http.StatusAccepted, result)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) job(writer http.ResponseWriter, request *http.Request) {
	var id, scopeType, scopeID, kind, state string
	var version, attempts, maxAttempts, updatedAtMS int64
	var errorCode sql.NullString
	var retryable sql.NullInt64
	err := server.database.QueryRowContext(request.Context(), `
SELECT id,
scope_type,
scope_id,
kind,
state,
version,
attempt_count,
max_attempts,
error_code,
error_retryable,
updated_at_ms
FROM jobs
WHERE id=?
`, request.PathValue("jobId")).
		Scan(
			&id,
			&scopeType,
			&scopeID,
			&kind,
			&state,
			&version,
			&attempts,
			&maxAttempts,
			&errorCode,
			&retryable,
			&updatedAtMS,
		)
	if errors.Is(err, sql.ErrNoRows) {
		server.notFound(writer, request)
		return
	}
	if err != nil {
		server.databaseError(writer, request, err)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, version))
	writeJSON(
		writer,
		http.StatusOK,
		map[string]any{
			"jobId":        id,
			"scopeType":    scopeType,
			"scopeId":      scopeID,
			"kind":         kind,
			"state":        state,
			"version":      version,
			"attemptCount": attempts,
			"maxAttempts":  maxAttempts,
			"errorCode":    nullableString(errorCode),
			"retryable":    retryable.Valid && retryable.Int64 == 1,
			"updatedAtMs":  updatedAtMS,
		},
	)
}

func (server *Server) jobEvents(writer http.ResponseWriter, request *http.Request) {
	server.streamJobEvents(writer, request, request.PathValue("jobId"))
}
