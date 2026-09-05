package packs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/cleanup"
)

const validationTimeout = 10 * time.Minute

func (service *Service) resumeQueuedJobs(ctx context.Context) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',attempt_count=0,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
 leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE kind='RUNTIME_ASSET_PACK_VALIDATE' AND state='RUNNING'
`, now)
	rows, err := service.database.QueryContext(ctx, `
SELECT id FROM jobs WHERE kind='RUNTIME_ASSET_PACK_VALIDATE' AND state='QUEUED'
ORDER BY created_at_ms,id
`)
	if err != nil {
		return
	}
	defer func() { cleanup.Error("close runtime pack resume jobs", rows.Close()) }()
	var jobIDs []string
	for rows.Next() {
		var jobID string
		if rows.Scan(&jobID) == nil {
			jobIDs = append(jobIDs, jobID)
		}
	}
	if rows.Err() != nil {
		return
	}
	for _, jobID := range jobIDs {
		go service.runJob(context.WithoutCancel(ctx), jobID)
	}
}

func (service *Service) runJob(parent context.Context, jobID string) {
	ctx, cancel := context.WithTimeout(parent, validationTimeout)
	defer cancel()
	claimed, err := service.claimJob(ctx, jobID)
	if err != nil || !claimed {
		return
	}
	diagnostic, validationErr := service.validateInstallation(ctx, jobID)
	if validationErr != nil {
		if diagnostic.Code == "" {
			diagnostic = Diagnostic{
				Code: "RPG_RUNTIME_PACK_VALIDATION_FAILED", Level: "ERROR",
				Message: "运行包验证任务未能完成",
			}
		}
		_ = service.finishJob(context.WithoutCancel(parent), jobID, diagnostic, false)
		return
	}
	_ = service.finishJob(context.WithoutCancel(parent), jobID, diagnostic, true)
}

func (service *Service) claimJob(ctx context.Context, jobID string) (bool, error) {
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',attempt_count=attempt_count+1,worker_id='runtime-pack-validator',
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,
 version=version+1,updated_at_ms=?
WHERE id=? AND kind='RUNTIME_ASSET_PACK_VALIDATE' AND state='QUEUED'
`, now, now+validationTimeout.Milliseconds(), now+validationTimeout.Milliseconds(), now, now, jobID)
	if err != nil {
		return false, fmt.Errorf("runtime pack claim: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return false, nil
	}
	_, err = service.database.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'STARTED','{}',? FROM jobs WHERE id=?
`, now, jobID)
	if err != nil {
		return true, fmt.Errorf("runtime pack claim event: %w", err)
	}
	return true, nil
}

func (service *Service) validateInstallation(
	ctx context.Context,
	jobID string,
) (Diagnostic, error) {
	var installationID, layout, generation, status string
	err := service.database.QueryRowContext(ctx, `
SELECT installation.id,definition.required_layout_version,definition.generation,installation.status
FROM jobs job
JOIN runtime_asset_pack_installations installation ON installation.id=job.scope_id
JOIN runtime_asset_pack_definitions definition ON definition.id=installation.definition_id
WHERE job.id=? AND job.kind='RUNTIME_ASSET_PACK_VALIDATE'
`, jobID).Scan(&installationID, &layout, &generation, &status)
	if err != nil || status != "VALIDATING" {
		return Diagnostic{}, fmt.Errorf("runtime pack validation snapshot: %w", err)
	}
	files, err := service.installationFiles(ctx, installationID)
	if err != nil {
		return Diagnostic{}, err
	}
	if layout != "easy-rtp-layout-v1" && layout != "mkxpz-v1" {
		return Diagnostic{Code: "RPG_RUNTIME_PACK_LAYOUT_INVALID", Level: "ERROR", Message: "未注册的运行包布局"}, ErrInvalid
	}
	if layout == "easy-rtp-layout-v1" {
		if err := ValidateEasyRTPLayout(generation, files); err != nil {
			message := "运行包目录、扩展名或已登记 RTP 资源不符合当前 EasyRPG 布局"
			var violation *layoutViolation
			if errors.As(err, &violation) {
				message = fmt.Sprintf(
					"运行包路径 %q %s", truncateRunes(violation.Path, 300), violation.Reason,
				)
			}
			return Diagnostic{
				Code: "RPG_RUNTIME_PACK_LAYOUT_INVALID", Level: "ERROR",
				Message: message,
			}, err
		}
	}
	return Diagnostic{
		Code: "RPG_RUNTIME_PACK_READY", Level: "INFO",
		Message: "运行包已通过安全结构和固定布局验证",
	}, nil
}

func (service *Service) installationFiles(ctx context.Context, installationID string) ([]FileIdentity, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT path,blob_id,size_bytes,sha256
FROM runtime_asset_pack_files WHERE installation_id=? ORDER BY ordinal
`, installationID)
	if err != nil {
		return nil, fmt.Errorf("runtime pack validation files: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack validation files", rows.Close()) }()
	var files []FileIdentity
	for rows.Next() {
		var file FileIdentity
		if err := rows.Scan(&file.Path, &file.BlobID, &file.SizeBytes, &file.SHA256); err != nil {
			return nil, fmt.Errorf("runtime pack validation file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtime pack validation rows: %w", err)
	}
	if len(files) == 0 {
		return nil, ErrInvalid
	}
	return files, nil
}

func (service *Service) finishJob(
	ctx context.Context,
	jobID string,
	diagnostic Diagnostic,
	succeeded bool,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("runtime pack finish: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var installationID, jobState string
	err = transaction.QueryRowContext(ctx, `
SELECT scope_id,state FROM jobs WHERE id=? AND kind='RUNTIME_ASSET_PACK_VALIDATE'
`, jobID).Scan(&installationID, &jobState)
	if err != nil || jobState != "RUNNING" {
		return fmt.Errorf("runtime pack finish state: %w", err)
	}
	diagnostics, _ := json.Marshal([]Diagnostic{diagnostic})
	now := service.now().UnixMilli()
	installationState, terminalEvent := "FAILED", "FAILED"
	var errorCode any = diagnostic.Code
	var retryable any = int64(0)
	if succeeded {
		installationState, terminalEvent, errorCode, retryable = "READY", "SUCCEEDED", nil, nil
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE runtime_asset_pack_installations
SET status=?,diagnostic_json=?,validated_at_ms=?,version=version+1
WHERE id=? AND status='VALIDATING'
`, installationState, string(diagnostics), now, installationID)
	if err != nil {
		return fmt.Errorf("runtime pack finish installation: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: installation", errFinishData)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs
SET state=?,finished_at_ms=?,error_code=?,error_retryable=?,heartbeat_at_ms=?,
 leased_until_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, terminalEvent, now, errorCode, retryable, now, now, jobID)
	if err != nil {
		return fmt.Errorf("runtime pack finish job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: job", errFinishData)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'RUNTIME_ASSET_PACK_INSTALLATION',?,?,json_object('code',?),?)
`, jobID, installationID, terminalEvent, diagnostic.Code, now)
	if err != nil {
		return fmt.Errorf("runtime pack finish event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("runtime pack finish commit: %w", err)
	}
	return nil
}
