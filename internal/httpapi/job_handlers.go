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
		server.importer.SyncParentAttachmentCancellation(request.Context(), result.JobID)
		_, _ = server.database.ExecContext(
			request.Context(),
			`
UPDATE dat_versions
SET parse_status='CANCELLED',
version=version+1,
updated_at_ms=?
WHERE id=(SELECT scope_id
FROM jobs
WHERE id=?
AND scope_type='DAT_VERSION'
AND kind='DAT_PARSE')
AND parse_status='PENDING'
`,
			server.now().UnixMilli(),
			result.JobID,
		)
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
	writeJSON(writer, http.StatusAccepted, result)
}
