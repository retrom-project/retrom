package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"retrom/internal/cleanup"
	"retrom/internal/service/jobs"
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
	result, pending, err := server.systemDeps.Jobs.Cancel(
		request.Context(),
		request.PathValue("jobId"),
		version,
		body.Reason,
	)
	if err != nil {
		if !errors.Is(err, jobs.ErrConflict) && !errors.Is(err, jobs.ErrRetryViaDomain) {
			server.databaseError(writer, request, err)
			return
		}
		writeError(
			writer,
			request,
			http.StatusConflict,
			"JOB_NOT_CANCELLABLE",
			"任务不可取消或版本已经变化",
			map[string]any{},
		)
		return
	}
	if !pending {
		server.importDeps.Importer.SyncImportGroupCancellation(request.Context(), result.JobID)
		server.importDeps.Importer.SyncParentAttachmentCancellation(request.Context(), result.JobID)
		server.importDeps.Importer.SyncMultiDiscAttachmentCancellation(request.Context(), result.JobID)
	} else {
		server.importDeps.Importer.CancelImportGroupJob(result.JobID)
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
	result, err := server.systemDeps.Jobs.Retry(request.Context(), request.PathValue("jobId"), version)
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
		writeError(
			writer,
			request,
			http.StatusConflict,
			"JOB_NOT_RETRYABLE",
			"任务不可重试或版本已经变化",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("ETag", fmt.Sprintf(`"v%d"`, result.Version))
	cleanup.Error("resume arcade attachments", server.importDeps.Importer.ResumeParentAttachmentJobs(request.Context()))
	cleanup.Error("resume multi-disc attachments",
		server.importDeps.Importer.ResumeMultiDiscAttachmentJobs(request.Context()))
	server.importDeps.Importer.ResumeImportGroupJobs(request.Context())
	writeJSON(writer, http.StatusAccepted, result)
	ctx := context.WithoutCancel(request.Context())
	afterIdempotencyCommit(writer, func() {
		switch result.Kind {
		case "VARIANT_VALIDATE":
			server.deferredWork.Go(func() { server.playDeps.Variants.Resume(ctx, result.JobID) })
		case "UPLOAD_FINALIZE":
			server.importDeps.Uploads.Resume(ctx, result.JobID)
		case "MEDIA_FETCH":
			server.reviewDeps.Metadata.ResumeMediaJob(ctx, result.JobID)
		}
	})
}
