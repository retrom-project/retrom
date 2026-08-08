package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/contentprofile"
	"retrom/internal/corevalidation"

	"github.com/google/uuid"
)

type validationInputs struct {
	GameVariantID         string `json:"gameVariantId"`
	GameContentRevisionID string `json:"gameContentRevisionId"`
	CoreArtifactID        string `json:"coreArtifactId"`
	DATVersionID          any    `json:"datVersionId"`
	ValidationInputDigest string `json:"validationInputDigest"`
	BIOSDependencyDigest  string `json:"biosDependencyDigest"`
}

type validationSnapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Scope         validationScope  `json:"scope"`
	ExecutionID   string           `json:"executionId"`
	Inputs        validationInputs `json:"inputs"`
}

type validationScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func validationDedupeKey(variantID, digest string) string {
	canonical, _ := json.Marshal(map[string]string{"gameVariantId": variantID, "validationInputDigest": digest})
	value := sha256.New()
	_, _ = value.Write([]byte("retrom-job-dedupe-v1\x00VARIANT_REVALIDATE\x00"))
	_, _ = value.Write(canonical)
	return hex.EncodeToString(value.Sum(nil))
}

//nolint:funlen,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) ensureVariant(
	ctx context.Context,
	request CreateRequest,
	requestedCore string,
	launchWhenReady bool,
) (Created, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var contentID, contentLogicalName, coreID, artifactID string
	var requiresThreads int
	var datID sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
COALESCE(f.logical_name,''),
c.id,
a.id,
c.requires_threads,
(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
LEFT JOIN game_content_files f ON f.game_content_revision_id=g.current_content_revision_id AND f.role='CONTENT'
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
ORDER BY f.sort_order,f.logical_name
LIMIT 1
`, request.GameID, requestedCore, requestedCore).
		Scan(&contentID, &contentLogicalName, &coreID, &artifactID, &requiresThreads, &datID)
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
	var variantID string
	err = transaction.QueryRowContext(ctx, `
SELECT id
FROM game_variants
WHERE game_id=?
AND core_id=?
`, request.GameID, coreID).
		Scan(&variantID)
	if errors.Is(err, sql.ErrNoRows) {
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
`, variantID, request.GameID, coreID, now, now); err != nil {
			return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
		}
	} else if err != nil {
		return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
	}
	biosSnapshot, _, _, err := corevalidation.ResolveBIOS(
		ctx,
		transaction,
		artifactID,
		contentLogicalName,
	)
	if err != nil {
		return Created{}, ErrBlocked
	}
	digest, err := corevalidation.ValidationInputDigest(artifactID, contentID, datID, biosSnapshot)
	if err != nil {
		return Created{}, ErrBlocked
	}
	biosSnapshotJSON, _ := biosSnapshot.JSON()
	biosSnapshotDigest := sha256.Sum256(biosSnapshotJSON)
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
		if existingStatus != "READY" {
			return Created{}, ErrBlocked
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET current_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND current_revision_id IS NOT ?
`, existingRevisionID, service.now().UnixMilli(), variantID, existingRevisionID); err != nil {
			return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return Created{}, fmt.Errorf("launch/ensure_variant: %w", err)
		}
		if launchWhenReady {
			return service.Create(ctx, request)
		}
		return Created{Status: "READY"}, nil
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
		hex.EncodeToString(biosSnapshotDigest[:]),
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

//nolint:funlen // Deduplication, lease recovery, job creation, and event emission share one transaction.
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
	err := transaction.QueryRowContext(ctx, `
SELECT id,
state
FROM jobs
WHERE kind='VARIANT_REVALIDATE'
AND dedupe_key=?
`, dedupeKey).
		Scan(&jobID, &jobState)
	if err == nil {
		if jobState == "FAILED" || jobState == "CANCELLED" {
			return "", false, ErrBlocked
		}
		return jobID, false, nil
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

// QueueDATRevalidations records every job in the caller's DAT activation
// transaction. Workers are resumed only after that transaction commits.
//
//nolint:funlen,gocognit // The DAT fan-out and per-variant deduplication share one consistent catalog snapshot.
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
	type target struct{ variantID, contentID string }
	targets := make([]target, 0)
	for rows.Next() {
		var item target
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
		var logicalName string
		if err := transaction.QueryRowContext(ctx, `
SELECT logical_name FROM game_content_files
WHERE game_content_revision_id=? AND role='CONTENT'
ORDER BY sort_order,logical_name LIMIT 1
`, item.contentID).Scan(&logicalName); err != nil {
			return 0, fmt.Errorf("launch/ensure_variant: %w", err)
		}
		biosSnapshot, _, _, err := corevalidation.ResolveBIOS(ctx, transaction, artifactID, logicalName)
		if err != nil {
			return 0, fmt.Errorf("launch/ensure_variant: %w", err)
		}
		digest, err := corevalidation.ValidationInputDigest(artifactID, item.contentID, targetDAT, biosSnapshot)
		if err != nil {
			return 0, fmt.Errorf("launch/ensure_variant: %w", err)
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
					return 0, fmt.Errorf("launch/ensure_variant: %w", err)
				}
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("launch/ensure_variant: %w", err)
		}
		_, created, err := service.queueValidationJob(
			ctx,
			transaction,
			item.variantID,
			item.contentID,
			artifactID,
			targetDAT,
			digest,
			hex.EncodeToString(biosDigest[:]),
		)
		if err != nil {
			return 0, err
		}
		if created {
			queued++
		}
	}
	return queued, nil
}

// ResumeQueuedValidationJobs is idempotent: each worker first claims its row
// with a state transition, so concurrent resume scans cannot duplicate work.
func (service *Service) ResumeQueuedValidationJobs() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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

//nolint:funlen,gocyclo // Core, artifact, DAT, BIOS, content, and launch-file checks form one readiness decision.
func (service *Service) validateVariant(
	parent context.Context,
	jobID, variantID, contentID, artifactID string,
	datID sql.NullString,
	digest string,
	biosDependencyDigest string,
) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=?,
execution_deadline_at_ms=?,
leased_until_ms=?,
heartbeat_at_ms=?,
worker_id='local',
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`,
		now,
		now+int64(30*time.Minute/time.Millisecond),
		now+60_000,
		now,
		now,
		jobID,
	)
	if err != nil {
		return
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return
	}
	_, _ = service.database.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'GAME_VARIANT',
?,
'STARTED',
'{}',
?)
`,
		jobID,
		variantID,
		now,
	)
	var contentLogicalName string
	if err := service.database.QueryRowContext(ctx, `
SELECT logical_name FROM game_content_files
WHERE game_content_revision_id=? AND role='CONTENT'
ORDER BY sort_order,logical_name LIMIT 1
`, contentID).Scan(&contentLogicalName); err != nil {
		return
	}
	biosSnapshot, biosStatus, biosCode, err := corevalidation.ResolveBIOS(
		ctx,
		service.database,
		artifactID,
		contentLogicalName,
	)
	if err != nil {
		return
	}
	biosSnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		return
	}
	biosDigest := sha256.Sum256(biosSnapshotJSON)
	currentDigest, err := corevalidation.ValidationInputDigest(artifactID, contentID, datID, biosSnapshot)
	if err != nil {
		return
	}
	status, code := service.validateContentForArtifact(ctx, contentID, artifactID, datID)
	if biosStatus != "READY" {
		status, code = biosStatus, biosCode
	}
	if currentDigest != digest || hex.EncodeToString(biosDigest[:]) != biosDependencyDigest {
		status, code = "BLOCKED", "LAUNCH_VALIDATION_INPUT_STALE"
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	revisionID := newUUID()
	var emulatorGameID any
	if status == "READY" {
		var next int64
		if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(emulator_game_id),
1000)+1
FROM game_variant_revisions
`).Scan(&next); err != nil {
			return
		}
		emulatorGameID = next
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variant_revisions(id,
game_variant_id,
game_content_revision_id,
core_artifact_id,
dat_version_id,
validation_input_digest,
emulator_game_id,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		revisionID,
		variantID,
		contentID,
		artifactID,
		nullableSQL(datID),
		digest,
		emulatorGameID,
		status,
		code,
		string(biosSnapshotJSON),
		service.now().UnixMilli(),
	); err != nil {
		return
	}
	for sortOrder, dependency := range biosSnapshot.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'BIOS_BUNDLE',?,?,?)
`, revisionID, dependency.LogicalName, *dependency.BlobID, sortOrder); err != nil {
			return
		}
	}
	jobState, eventType := "FAILED", "FAILED"
	var errorCode any = code
	var retryable any = 0
	if status == "READY" {
		jobState, eventType, errorCode, retryable = "SUCCEEDED", "SUCCEEDED", nil, nil
		if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET current_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, revisionID, service.now().UnixMilli(), variantID); err != nil {
			return
		}
	}
	finished := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state=?,
error_code=?,
error_retryable=?,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`, jobState, errorCode, retryable, finished, finished, jobID); err != nil {
		return
	}
	data, _ := json.Marshal(map[string]any{"code": code, "variantRevisionId": revisionID})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'GAME_VARIANT',
?,
?,
?,
?)
`, jobID, variantID, eventType, string(data), finished); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) validateContentForArtifact(
	ctx context.Context,
	contentID, artifactID string,
	datID sql.NullString,
) (string, string) {
	var coreID, platformID, logicalName string
	var relationshipEnabled int
	err := service.database.QueryRowContext(ctx, `
SELECT a.core_id,
pi.platform_id,
f.logical_name,
EXISTS(SELECT 1
FROM platform_cores pc
WHERE pc.platform_id=pi.platform_id
AND pc.core_id=a.core_id
AND pc.enabled=1)
FROM core_artifacts a
JOIN game_content_files f ON f.game_content_revision_id=?
AND f.role='CONTENT'
JOIN game_content_revisions cr ON cr.id=f.game_content_revision_id
JOIN games g ON g.id=cr.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE a.id=?
ORDER BY f.sort_order,
f.logical_name LIMIT 1
`, contentID, artifactID).
		Scan(&coreID, &platformID, &logicalName, &relationshipEnabled)
	if err != nil {
		return "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
	}
	if relationshipEnabled != 1 {
		return "INCOMPATIBLE", "CORE_PLATFORM_UNSUPPORTED"
	}
	if _, exists := contentprofile.ByPlatform(platformID); exists {
		if !contentprofile.AcceptsRaw(platformID, logicalName) {
			return "INCOMPATIBLE", "CORE_CONTENT_FORMAT_UNSUPPORTED"
		}
	} else if platformID != "arcade" && platformID != "dos" {
		return "BLOCKED", "CORE_CONTENT_PROFILE_MISSING"
	}
	if status, code := service.validateStaticBIOSForContent(ctx, artifactID, logicalName); status != "READY" {
		return status, code
	}
	if coreID == "fbneo" || coreID == "mame2003" || coreID == "mame2003_plus" {
		if !datID.Valid || !strings.EqualFold(filepath.Ext(logicalName), ".zip") {
			return "INCOMPATIBLE", "ARCADE_CONTENT_NOT_ROMSET"
		}
		machine := strings.TrimSuffix(filepath.Base(logicalName), filepath.Ext(logicalName))
		var classification string
		if err := service.database.QueryRowContext(ctx, `
SELECT classification
FROM dat_machines
WHERE dat_version_id=?
AND lower(machine_name)=lower(?)
`, datID.String, machine).Scan(&classification); err != nil ||
			classification != "NORMAL" {
			return "INCOMPATIBLE", "ARCADE_MACHINE_NOT_FOUND"
		}
	}
	return "READY", "READY"
}

func (service *Service) validateStaticBIOSForContent(
	ctx context.Context,
	artifactID, logicalName string,
) (string, string) {
	_, status, code, err := corevalidation.ResolveBIOS(ctx, service.database, artifactID, logicalName)
	if err != nil {
		return "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
	}
	return status, code
}

func nullableSQL(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func newUUID() string {
	value, _ := uuid.NewV7()
	return value.String()
}
