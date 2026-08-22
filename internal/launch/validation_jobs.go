package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
)

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) ensureVariant(
	ctx context.Context,
	profileID string,
	request CreateRequest,
	requestedCore string,
	launchWhenReady bool,
) (Created, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var contentID, contentLogicalName, contentKind, coreID, artifactID string
	var requiresThreads int
	var datID sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
COALESCE(f.logical_name,''),
content.content_kind,
c.id,
a.id,
c.requires_threads,
(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN game_content_revisions content ON content.id=g.current_content_revision_id
LEFT JOIN game_content_files f ON f.game_content_revision_id=g.current_content_revision_id
AND f.role IN ('CONTENT','DISC')
JOIN platform_cores pc ON pc.platform_id=pi.platform_id
AND pc.enabled=1
JOIN cores c ON c.id=pc.core_id
AND c.enabled=1
JOIN core_artifacts a ON a.core_id=c.id
AND a.enabled=1
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
AND c.id=CASE WHEN ?='' THEN pi.default_core_id ELSE ? END
ORDER BY CASE f.role WHEN 'CONTENT' THEN 0 ELSE 1 END,f.sort_order,f.logical_name
LIMIT 1
`, request.GameID, requestedCore, requestedCore).
		Scan(&contentID, &contentLogicalName, &contentKind, &coreID, &artifactID, &requiresThreads, &datID)
	if err != nil ||
		requiresThreads == 1 &&
			(!request.ClientCapabilities.SecureContext ||
				!request.ClientCapabilities.CrossOriginIsolated ||
				!request.ClientCapabilities.SharedArrayBuffer) {
		return Created{}, ErrBlocked
	}
	if service.dependencies.Versions[service.dependencies.Active.Manifest.EmulatorJS.Version] == nil {
		return Created{}, ErrBlocked
	}
	variantID, err := service.ensureGameVariant(ctx, transaction, request.GameID, coreID)
	if err != nil {
		return Created{}, err
	}
	digest, biosDependencyDigest, err := service.validationDigests(
		ctx, transaction, variantID, contentID, contentLogicalName, contentKind, artifactID, datID,
	)
	if err != nil {
		return Created{}, ErrBlocked
	}
	var existingRevisionID, existingStatus string
	err = transaction.QueryRowContext(ctx, `
SELECT id,
status
FROM game_variant_revisions
WHERE game_variant_id=?
AND validation_input_digest=?
`, variantID, digest).
		Scan(&existingRevisionID, &existingStatus)
	if err == nil {
		return service.useExistingValidation(
			ctx, transaction, profileID, request, variantID, existingRevisionID, existingStatus, launchWhenReady,
		)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	jobID, _, err := service.queueValidationJob(
		ctx,
		transaction,
		variantID,
		contentID,
		artifactID,
		datID,
		digest,
		biosDependencyDigest,
	)
	if err != nil {
		return Created{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if launchWhenReady {
		go service.resumeValidationJob(context.WithoutCancel(ctx), jobID)
	}
	return Created{Status: "VALIDATION_PENDING", JobID: jobID, RetryAfterMS: 1000}, nil
}

func (service *Service) ensureGameVariant(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, coreID string,
) (string, error) {
	var variantID string
	err := transaction.QueryRowContext(ctx, `
SELECT id
FROM game_variants
WHERE game_id=?
AND core_id=?
`, gameID, coreID).Scan(&variantID)
	if err == nil {
		return variantID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("launch/ensure_variant: %w", err)
	}
	variantID = newUUID()
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variants(id,
game_id,
core_id,
current_revision_id,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
NULL,
1,
?,
?)
`, variantID, gameID, coreID, now, now); err != nil {
		return "", fmt.Errorf("launch/ensure_variant: %w", err)
	}
	return variantID, nil
}

func (service *Service) useExistingValidation(
	ctx context.Context,
	transaction *sql.Tx,
	profileID string,
	request CreateRequest,
	variantID, revisionID, status string,
	launchWhenReady bool,
) (Created, error) {
	if status != "READY" {
		return Created{}, ErrBlocked
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET current_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND current_revision_id IS NOT ?
`, revisionID, service.now().UnixMilli(), variantID, revisionID); err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if launchWhenReady {
		return service.Create(ctx, profileID, request)
	}
	return Created{Status: "READY"}, nil
}

// ResumeValidationJob resumes one queued validation. Claiming the Job is the
// idempotency boundary, so duplicate resume signals are harmless.
func (service *Service) ResumeValidationJob(ctx context.Context, jobID string) {
	service.resumeValidationJob(ctx, jobID)
}

// EnsureVariantForMove never creates a LaunchSession. It either returns the
// shared validation Job or makes an already validated revision current.
func (service *Service) EnsureVariantForMove(ctx context.Context, gameID, coreID string) (Created, error) {
	selected := coreID
	return service.ensureVariant(
		ctx,
		"",
		CreateRequest{
			GameID:             gameID,
			CoreID:             &selected,
			ReturnTo:           "/games/" + gameID,
			ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
		},
		coreID,
		false,
	)
}

// Deduplication, lease recovery, job creation, and event emission share one transaction.
func (service *Service) queueValidationJob(
	ctx context.Context,
	transaction *sql.Tx,
	variantID, contentID, artifactID string,
	datID sql.NullString,
	digest string,
	biosDependencyDigest string,
) (string, bool, error) {
	dedupeKey := validationDedupeKey(variantID, digest)
	var jobID, jobState string
	var retryable sql.NullInt64
	var executionNo, jobVersion int64
	err := transaction.QueryRowContext(ctx, `
SELECT id,
state,
error_retryable,
execution_no,
version
FROM jobs
WHERE kind='VARIANT_REVALIDATE'
AND dedupe_key=?
`, dedupeKey).
		Scan(&jobID, &jobState, &retryable, &executionNo, &jobVersion)
	if err == nil {
		return service.reuseValidationJob(
			ctx, transaction, variantID, jobID, jobState, retryable, executionNo, jobVersion,
		)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	jobID = newUUID()
	executionID := newUUID()
	now := service.now().UnixMilli()
	payload, _ := json.Marshal(map[string]any{"schemaVersion": 1, "inputExecutionNo": 1})
	snapshot := validationSnapshot{
		SchemaVersion: 1,
		Kind:          "VARIANT_REVALIDATE",
		Scope:         validationScope{Type: "GAME_VARIANT", ID: variantID},
		ExecutionID:   executionID,
		Inputs: validationInputs{
			GameVariantID:         variantID,
			GameContentRevisionID: contentID,
			CoreArtifactID:        artifactID,
			DATVersionID:          nullableSQL(datID),
			ValidationInputDigest: digest,
			BIOSDependencyDigest:  biosDependencyDigest,
		},
	}
	inputJSON, _ := json.Marshal(snapshot)
	inputHash := sha256.Sum256(inputJSON)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'VARIANT_REVALIDATE',
?,
1,
?,
0,
'QUEUED',
0,
2,
?,
?,
?)
`, jobID, variantID, dedupeKey, string(payload), now, now, now); err != nil {
		return "", false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,
execution_no,
input_json,
input_digest,
created_at_ms) VALUES(?,
1,
?,
?,
?)
`, jobID, string(inputJSON), hex.EncodeToString(inputHash[:]), now); err != nil {
		return "", false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'QUEUED',
'{}',
?)
`, jobID, variantID, now); err != nil {
		return "", false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	return jobID, true, nil
}

func (service *Service) reuseValidationJob(
	ctx context.Context,
	transaction *sql.Tx,
	variantID, jobID, jobState string,
	retryable sql.NullInt64,
	executionNo, jobVersion int64,
) (string, bool, error) {
	if jobState == "FAILED" && retryable.Valid && retryable.Int64 == 1 {
		if err := retryVariantValidationJob(
			ctx, transaction, jobID, variantID, executionNo, jobVersion, service.now().UnixMilli(),
		); err != nil {
			return "", false, err
		}
		return jobID, true, nil
	}
	if jobState == "FAILED" || jobState == "CANCELLED" {
		return "", false, ErrBlocked
	}
	return jobID, false, nil
}

func retryVariantValidationJob(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, variantID string,
	executionNo, jobVersion, now int64,
) error {
	var previousJSON string
	if err := transaction.QueryRowContext(ctx, `
SELECT input_json FROM job_input_snapshots WHERE job_id=? AND execution_no=?
`, jobID, executionNo).Scan(&previousJSON); err != nil {
		return fmt.Errorf("launch/retry validation input: %w", err)
	}
	var snapshot validationSnapshot
	if err := json.Unmarshal([]byte(previousJSON), &snapshot); err != nil ||
		snapshot.SchemaVersion != 1 || snapshot.Kind != "VARIANT_REVALIDATE" ||
		snapshot.Scope.Type != "GAME_VARIANT" || snapshot.Scope.ID != variantID {
		return ErrBlocked
	}
	executionNo++
	snapshot.ExecutionID = newUUID()
	inputJSON, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("launch/retry validation input: %w", err)
	}
	inputHash := sha256.Sum256(inputJSON)
	payload, _ := json.Marshal(map[string]any{"schemaVersion": 1, "inputExecutionNo": executionNo})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)
`, jobID, executionNo, string(inputJSON), hex.EncodeToString(inputHash[:]), now); err != nil {
		return fmt.Errorf("launch/write retried validation input: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',execution_no=?,payload_json=?,attempt_count=0,available_at_ms=?,
execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
finished_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND state='FAILED' AND error_retryable=1
`, executionNo, string(payload), now, now, jobID, jobVersion)
	if err != nil {
		return fmt.Errorf("launch/reset retried validation job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return ErrBlocked
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,'RETRY_SCHEDULED',json_object('schemaVersion',1,'executionNo',?,'trigger','LAUNCH'),?)
`, jobID, variantID, executionNo, now); err != nil {
		return fmt.Errorf("launch/write retried validation event: %w", err)
	}
	return nil
}

type datRevalidationTarget struct{ variantID, contentID string }

// QueueDATRevalidations records every job in the caller's DAT activation
// transaction. Workers are resumed only after that transaction commits.
//
// The DAT fan-out and per-variant deduplication share one consistent catalog snapshot.
func (service *Service) QueueDATRevalidations(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID, datID string,
) (int64, error) {
	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT v.id,
g.current_content_revision_id
FROM game_variants v
JOIN games g ON g.id=v.game_id
JOIN game_variant_revisions r ON r.id=v.current_revision_id
WHERE r.core_artifact_id=?
AND g.status='PUBLISHED'
ORDER BY v.id
`,
		artifactID,
	)
	if err != nil {
		return 0, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	targets := make([]datRevalidationTarget, 0)
	for rows.Next() {
		var item datRevalidationTarget
		if err := rows.Scan(&item.variantID, &item.contentID); err != nil {
			return 0, fmt.Errorf("launch/ensure_variant: %w", err)
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	queued := int64(0)
	targetDAT := sql.NullString{String: datID, Valid: true}
	for _, item := range targets {
		created, err := service.queueDATRevalidationTarget(ctx, transaction, artifactID, targetDAT, item)
		if err != nil {
			return 0, err
		}
		if created {
			queued++
		}
	}
	return queued, nil
}

func (service *Service) queueDATRevalidationTarget(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID string,
	targetDAT sql.NullString,
	item datRevalidationTarget,
) (bool, error) {
	var logicalName string
	if err := transaction.QueryRowContext(ctx, `
SELECT logical_name FROM game_content_files
WHERE game_content_revision_id=? AND role='CONTENT'
ORDER BY sort_order,logical_name LIMIT 1
`, item.contentID).Scan(&logicalName); err != nil {
		return false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	biosSnapshot, _, _, err := corevalidation.ResolveBIOS(ctx, transaction, artifactID, logicalName)
	if err != nil {
		return false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	digest, err := corevalidation.ValidationInputDigest(artifactID, item.contentID, targetDAT, biosSnapshot)
	if err != nil {
		return false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	biosJSON, _ := biosSnapshot.JSON()
	biosDigest := sha256.Sum256(biosJSON)
	var revisionID, status string
	err = transaction.QueryRowContext(ctx, `
SELECT id,
status
FROM game_variant_revisions
WHERE game_variant_id=?
AND validation_input_digest=?
`, item.variantID, digest).
		Scan(&revisionID, &status)
	if err == nil {
		if status == "READY" {
			if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET current_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND current_revision_id<>?
`, revisionID, service.now().UnixMilli(), item.variantID, revisionID); err != nil {
				return false, fmt.Errorf("launch/ensure_variant: %w", err)
			}
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	_, created, err := service.queueValidationJob(
		ctx, transaction, item.variantID, item.contentID, artifactID,
		targetDAT, digest, hex.EncodeToString(biosDigest[:]),
	)
	return created, err
}

// ResumeQueuedValidationJobs is idempotent: each worker first claims its row
// with a state transition, so concurrent resume scans cannot duplicate work.
func (service *Service) ResumeQueuedValidationJobs() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service.recoverStaleValidationJobs(ctx)
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT id
FROM jobs
WHERE kind='VARIANT_REVALIDATE'
AND state='QUEUED'
ORDER BY created_at_ms,
id
`,
	)
	if err != nil {
		return
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	jobIDs := make([]string, 0)
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			cleanup.Error("scan queued validation job", err)
			return
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Err(); err != nil {
		cleanup.Error("iterate queued validation jobs", err)
		return
	}
	for _, jobID := range jobIDs {
		go service.resumeValidationJob(context.Background(), jobID)
	}
}

func (service *Service) recoverStaleValidationJobs(ctx context.Context) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE jobs
SET state='FAILED',
error_code='LAUNCH_CORE_VALIDATION_UNAVAILABLE',
error_retryable=1,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE kind='VARIANT_REVALIDATE'
AND state='RUNNING'
AND leased_until_ms<?
AND attempt_count>=max_attempts;

UPDATE jobs
SET state='QUEUED',
available_at_ms=?,
execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,
leased_until_ms=NULL,
heartbeat_at_ms=NULL,
worker_id=NULL,
version=version+1,
updated_at_ms=?
WHERE kind='VARIANT_REVALIDATE'
AND state='RUNNING'
AND leased_until_ms<?
AND attempt_count<max_attempts
`, now, now, now, now, now, now)
}

func (service *Service) resumeValidationJob(parent context.Context, jobID string) {
	var inputJSON string
	if err := service.database.QueryRowContext(parent, `
SELECT s.input_json
FROM jobs j
JOIN job_input_snapshots s ON s.job_id=j.id
AND s.execution_no=j.execution_no
WHERE j.id=?
AND j.kind='VARIANT_REVALIDATE'
`, jobID).Scan(&inputJSON); err != nil {
		return
	}
	var snapshot validationSnapshot
	if err := json.Unmarshal([]byte(inputJSON), &snapshot); err != nil || snapshot.SchemaVersion != 1 ||
		snapshot.Kind != "VARIANT_REVALIDATE" {
		return
	}
	datID := sql.NullString{}
	if value, ok := snapshot.Inputs.DATVersionID.(string); ok && value != "" {
		datID = sql.NullString{String: value, Valid: true}
	}
	service.validateVariant(
		parent,
		jobID,
		snapshot.Inputs.GameVariantID,
		snapshot.Inputs.GameContentRevisionID,
		snapshot.Inputs.CoreArtifactID,
		datID,
		snapshot.Inputs.ValidationInputDigest,
		snapshot.Inputs.BIOSDependencyDigest,
	)
}
