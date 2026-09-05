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

	"retrom/internal/contentcapability"

	"retrom/internal/cleanup"
)

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

	var gameID, contentLogicalName, contentKind, coreID string
	var providerID, targetID, sourceManifestDigest string
	var contentPolicy contentcapability.Policy
	var gameVersion int64
	var datID sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT game.id,
COALESCE(file.logical_name,''),
game.content_kind,
core.id,
binding.provider_id,
binding.target_id,
`+contentcapability.BindingPolicySQL+`,
(SELECT id FROM dat_versions
 WHERE provider_id=binding.provider_id AND target_id=binding.target_id AND is_active=1),
game.version,
game.source_manifest_digest
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
LEFT JOIN game_files file ON file.game_id=game.id AND file.role IN ('CONTENT','DISC')
JOIN platform_cores platform_core ON platform_core.platform_id=instance.platform_id AND platform_core.enabled=1
JOIN cores core ON core.id=platform_core.core_id AND core.enabled=1
JOIN runtime_target_bindings binding ON binding.core_id=core.id AND binding.launch_policy!='DISABLED'
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id AND binding_platform.core_id=core.id
JOIN runtime_binding_content_kinds binding_kind ON binding_kind.binding_id=binding.binding_id
 AND binding_kind.content_kind=game.content_kind
WHERE game.id=? AND game.status='PUBLISHED' AND instance.enabled=1
AND core.id=CASE WHEN ?='' THEN instance.default_core_id ELSE ? END
ORDER BY CASE file.role WHEN 'CONTENT' THEN 0 ELSE 1 END,file.sort_order,file.logical_name
LIMIT 1
`, request.GameID, requestedCore, requestedCore).Scan(
		&gameID, &contentLogicalName, &contentKind, &coreID, &providerID, &targetID,
		&contentPolicy, &datID, &gameVersion, &sourceManifestDigest,
	)
	target, targetExists := service.runtimeBuilder.Target(providerID, targetID)
	if err != nil || !targetExists ||
		!validThreadCapabilities(target.Capabilities.RequiresThreads, request.ClientCapabilities) {
		return Created{}, ErrBlocked
	}
	variantID, err := service.ensureGameVariant(
		ctx, transaction, gameID, coreID, providerID, targetID, datID,
	)
	if err != nil {
		return Created{}, err
	}
	baseDigest, biosDependencyDigest, err := service.validationDigests(
		ctx, transaction, variantID, gameID, contentLogicalName, contentKind,
		providerID, targetID, contentPolicy, datID,
	)
	if err != nil {
		return Created{}, ErrBlocked
	}
	digest := bindCurrentGameStateDigest(baseDigest, gameVersion, sourceManifestDigest)
	jobID, queued, err := service.queueValidationJob(
		ctx, transaction, variantID, gameID, gameVersion, sourceManifestDigest,
		providerID, targetID, contentPolicy, datID, digest, biosDependencyDigest,
	)
	if err != nil {
		return Created{}, err
	}
	if queued {
		if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants SET status='BLOCKED',compatibility_code='VALIDATION_PENDING',
emulator_game_id=NULL,version=version+1,updated_at_ms=? WHERE id=?
`, service.now().UnixMilli(), variantID); err != nil {
			return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
		}
	}
	var status string
	if err := transaction.QueryRowContext(
		ctx, `SELECT status FROM game_variants WHERE id=?`, variantID,
	).Scan(&status); err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if status == "READY" {
		if launchWhenReady {
			return service.Create(ctx, profileID, request)
		}
		return Created{Status: "READY"}, nil
	}
	if launchWhenReady {
		go service.resumeValidationJob(context.WithoutCancel(ctx), jobID)
	}
	return Created{Status: "VALIDATION_PENDING", JobID: jobID, RetryAfterMS: 1000}, nil
}

func bindCurrentGameStateDigest(baseDigest string, gameVersion int64, sourceManifestDigest string) string {
	canonical, _ := json.Marshal(struct {
		BaseDigest           string `json:"baseDigest"`
		GameVersion          int64  `json:"gameVersion"`
		SourceManifestDigest string `json:"sourceManifestDigest"`
	}{baseDigest, gameVersion, sourceManifestDigest})
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func (service *Service) ensureGameVariant(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, coreID, providerID, targetID string,
	datID sql.NullString,
) (string, error) {
	var variantID string
	err := transaction.QueryRowContext(ctx, `
SELECT id FROM game_variants WHERE game_id=? AND core_id=?
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
INSERT INTO game_variants(
 id,game_id,core_id,provider_id,target_id,dat_version_id,emulator_game_id,
 status,compatibility_code,dependency_snapshot_json,default_dos_entry,
 version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,NULL,'BLOCKED','VALIDATION_PENDING','{}',NULL,1,?,?)
`, variantID, gameID, coreID, providerID, targetID, nullableSQL(datID), now, now); err != nil {
		return "", fmt.Errorf("launch/ensure_variant: %w", err)
	}
	return variantID, nil
}

// ResumeValidationJob resumes one queued validation. Claiming the job is the
// idempotency boundary, so duplicate resume signals are harmless.
func (service *Service) ResumeValidationJob(ctx context.Context, jobID string) {
	service.resumeValidationJob(ctx, jobID)
}

// EnsureVariantForMove validates the requested current core without creating a launch session.
func (service *Service) EnsureVariantForMove(ctx context.Context, gameID, coreID string) (Created, error) {
	selected := coreID
	return service.ensureVariant(ctx, "", CreateRequest{
		GameID: gameID, CoreID: &selected, ReturnTo: "/games/" + gameID,
		ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
	}, coreID, false)
}

func (service *Service) queueValidationJob(
	ctx context.Context,
	transaction *sql.Tx,
	variantID, gameID string,
	gameVersion int64,
	sourceManifestDigest, providerID, targetID string,
	contentPolicy contentcapability.Policy,
	datID sql.NullString,
	digest, biosDependencyDigest string,
) (string, bool, error) {
	dedupeKey := validationDedupeKey(variantID, digest)
	var jobID, jobState string
	var retryable sql.NullInt64
	var executionNo, jobVersion int64
	err := transaction.QueryRowContext(ctx, `
SELECT id,state,error_retryable,execution_no,version
FROM jobs WHERE kind='VARIANT_VALIDATE' AND dedupe_key=?
`, dedupeKey).Scan(&jobID, &jobState, &retryable, &executionNo, &jobVersion)
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
		Kind:          "VARIANT_VALIDATE",
		Scope:         validationScope{Type: "GAME_VARIANT", ID: variantID},
		ExecutionID:   executionID,
		Inputs: validationInputs{
			GameID: gameID, GameVariantID: variantID, GameVersion: gameVersion,
			SourceManifestDigest: sourceManifestDigest,
			ProviderID:           providerID, TargetID: targetID, ContentPolicy: contentPolicy,
			DATVersionID: nullableSQL(datID), ValidationInputDigest: digest,
			BIOSDependencyDigest: biosDependencyDigest,
		},
	}
	inputJSON, _ := json.Marshal(snapshot)
	inputHash := sha256.Sum256(inputJSON)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'GAME_VARIANT',?,'VARIANT_VALIDATE',?,1,?,0,'QUEUED',0,2,?,?,?)
`, jobID, variantID, dedupeKey, string(payload), now, now, now); err != nil {
		return "", false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, jobID, string(inputJSON), hex.EncodeToString(inputHash[:]), now); err != nil {
		return "", false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,'QUEUED','{}',?)
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
		snapshot.SchemaVersion != 1 || snapshot.Kind != "VARIANT_VALIDATE" ||
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

type datValidationTarget struct{ variantID, gameID string }

// QueueDATRevalidations validates affected current Arcade variants against the newly active DAT.
func (service *Service) QueueDATRevalidations(
	ctx context.Context,
	transaction *sql.Tx,
	providerID, targetID, datID string,
) (int64, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT variant.id,game.id
FROM game_variants variant JOIN games game ON game.id=variant.game_id
WHERE variant.provider_id=? AND variant.target_id=? AND game.status='PUBLISHED'
ORDER BY variant.id
`, providerID, targetID)
	if err != nil {
		return 0, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	targets := make([]datValidationTarget, 0)
	for rows.Next() {
		var item datValidationTarget
		if err := rows.Scan(&item.variantID, &item.gameID); err != nil {
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
		created, err := service.queueDATValidationTarget(ctx, transaction, providerID, targetID, targetDAT, item)
		if err != nil {
			return 0, err
		}
		if created {
			queued++
		}
	}
	return queued, nil
}

func (service *Service) queueDATValidationTarget(
	ctx context.Context,
	transaction *sql.Tx,
	providerID, targetID string,
	targetDAT sql.NullString,
	item datValidationTarget,
) (bool, error) {
	var logicalName, contentKind, sourceManifestDigest string
	var contentPolicy contentcapability.Policy
	var gameVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE((SELECT logical_name FROM game_files
 WHERE game_id=game.id AND role IN ('CONTENT','DISC')
 ORDER BY CASE role WHEN 'CONTENT' THEN 0 ELSE 1 END,sort_order,logical_name LIMIT 1),''),
game.content_kind,game.version,game.source_manifest_digest,
`+contentcapability.BindingPolicySQL+`
FROM games game
JOIN game_variants variant ON variant.id=? AND variant.game_id=game.id
JOIN runtime_target_bindings binding ON binding.core_id=variant.core_id
 AND binding.provider_id=? AND binding.target_id=? AND binding.launch_policy!='DISABLED'
LIMIT 1
`, item.variantID, providerID, targetID).Scan(
		&logicalName, &contentKind, &gameVersion, &sourceManifestDigest, &contentPolicy,
	); err != nil {
		return false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	baseDigest, biosDigest, err := service.validationDigests(
		ctx, transaction, item.variantID, item.gameID, logicalName, contentKind,
		providerID, targetID, contentPolicy, targetDAT,
	)
	if err != nil {
		return false, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	digest := bindCurrentGameStateDigest(baseDigest, gameVersion, sourceManifestDigest)
	_, created, err := service.queueValidationJob(
		ctx, transaction, item.variantID, item.gameID, gameVersion, sourceManifestDigest,
		providerID, targetID, contentPolicy, targetDAT, digest, biosDigest,
	)
	if err != nil {
		return false, err
	}
	if created {
		if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET dat_version_id=?,status='BLOCKED',compatibility_code='VALIDATION_PENDING',
emulator_game_id=NULL,version=version+1,updated_at_ms=?
WHERE id=?
`, targetDAT.String, service.now().UnixMilli(), item.variantID); err != nil {
			return false, fmt.Errorf("launch/ensure_variant: %w", err)
		}
	}
	return created, nil
}

// ResumeQueuedValidationJobs is idempotent: each worker first claims its row.
func (service *Service) ResumeQueuedValidationJobs() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service.recoverStaleValidationJobs(ctx)
	rows, err := service.database.QueryContext(ctx, `
SELECT id FROM jobs WHERE kind='VARIANT_VALIDATE' AND state='QUEUED' ORDER BY created_at_ms,id
`)
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
SET state='FAILED',error_code='LAUNCH_CORE_VALIDATION_UNAVAILABLE',error_retryable=1,
finished_at_ms=?,leased_until_ms=NULL,version=version+1,updated_at_ms=?
WHERE kind='VARIANT_VALIDATE' AND state='RUNNING' AND leased_until_ms<? AND attempt_count>=max_attempts;

UPDATE jobs
SET state='QUEUED',available_at_ms=?,execution_started_at_ms=NULL,execution_deadline_at_ms=NULL,
leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,version=version+1,updated_at_ms=?
WHERE kind='VARIANT_VALIDATE' AND state='RUNNING' AND leased_until_ms<? AND attempt_count<max_attempts
`, now, now, now, now, now, now)
}

func (service *Service) resumeValidationJob(parent context.Context, jobID string) {
	var inputJSON string
	if err := service.database.QueryRowContext(parent, `
SELECT snapshot.input_json
FROM jobs job JOIN job_input_snapshots snapshot
 ON snapshot.job_id=job.id AND snapshot.execution_no=job.execution_no
WHERE job.id=? AND job.kind='VARIANT_VALIDATE'
`, jobID).Scan(&inputJSON); err != nil {
		return
	}
	var snapshot validationSnapshot
	if err := json.Unmarshal([]byte(inputJSON), &snapshot); err != nil ||
		snapshot.SchemaVersion != 1 || snapshot.Kind != "VARIANT_VALIDATE" {
		return
	}
	datID := sql.NullString{}
	if value, ok := snapshot.Inputs.DATVersionID.(string); ok && value != "" {
		datID = sql.NullString{String: value, Valid: true}
	}
	service.validateVariant(parent, jobID, snapshot.Inputs, datID)
}
