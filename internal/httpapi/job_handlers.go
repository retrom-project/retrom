package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/jobs"
)

func (server *Server) cancelJob(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(writer, request, &body, 8<<10) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "取消原因无效", map[string]any{})
		return
	}
	result, pending, err := server.jobService.Cancel(
		request.Context(),
		request.PathValue("jobId"),
		version,
		body.Reason,
	)
	if err != nil {
		writeError(writer, request, http.StatusConflict, "JOB_NOT_CANCELLABLE", "任务不可取消或版本已经变化", map[string]any{})
		return
	}
	if !pending {
		server.importer.SyncImportGroupCancellation(request.Context(), result.JobID)
		server.importer.SyncParentAttachmentCancellation(request.Context(), result.JobID)
		server.importer.SyncMultiDiscAttachmentCancellation(request.Context(), result.JobID)
	} else {
		server.importer.CancelImportGroupJob(result.JobID)
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	status := http.StatusOK
	if pending {
		status = http.StatusAccepted
	}
	writeJSON(writer, status, result)
}

func (server *Server) retryJob(writer http.ResponseWriter, request *http.Request) {
	version, ok := requireVersion(writer, request)
	if !ok {
		return
	}
	if !validIdempotencyKey(request.Header.Get("Idempotency-Key")) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键无效", map[string]any{})
		return
	}
	var body struct{}
	if decodeJSON(writer, request, &body, 1024) != nil {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "重试请求无效", map[string]any{})
		return
	}
	result, err := server.jobService.Retry(request.Context(), request.PathValue("jobId"), version)
	if errors.Is(err, jobs.ErrRetryViaDomain) {
		writeError(
			writer,
			request,
			http.StatusConflict,
			"RETRY_VIA_DOMAIN_ACTION",
			"该任务必须从所属审核或游戏操作重新创建",
			map[string]any{},
		)
		return
	}
	if err != nil {
		writeError(writer, request, http.StatusConflict, "JOB_NOT_RETRYABLE", "任务不可重试或版本已经变化", map[string]any{})
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	server.importer.ResumeParentAttachmentJobs(request.Context())
	server.importer.ResumeMultiDiscAttachmentJobs(request.Context())
	server.importer.ResumeImportGroupJobs(request.Context())
	writeJSON(writer, http.StatusAccepted, result)
}
