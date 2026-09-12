package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/persistence/contentquery"
	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"

	"retrom/internal/dbexec"

	"retrom/internal/persistence/recordstore"

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
	if launchWhenReady {
		return service.Create(ctx, profileID, request)
	}
	result, err := service.productCreator(persistence.NewProductCreation(service.database)).EnsureVariant(
		ctx,
		request.GameID,
		requestedCore,
		request.ClientCapabilities,
	)
	if err != nil {
		return Created{}, fmt.Errorf("launch ensure variant: %w", err)
	}
	return result, nil
}

func bindCurrentGameStateDigest(baseDigest string, gameVersion int64, sourceManifestDigest string) string {
	return application.BindCurrentGameStateDigest(baseDigest, gameVersion, sourceManifestDigest)
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
	ctx context.Context, transaction *sql.Tx, variantID, gameID string, gameVersion int64,
	sourceManifestDigest, providerID, targetID string, contentPolicy contentcapability.Policy,
	datID sql.NullString, digest, biosDependencyDigest string,
) (string, bool, error) {
	scheduler := application.NewValidationScheduler(
		persistence.NewValidationJobs(transaction), application.ValidationEnvironment{Now: service.now},
	)
	result, err := scheduler.Queue(
		ctx,
		application.ValidationInputs{
			GameID: gameID, GameVariantID: variantID, GameVersion: gameVersion, SourceManifestDigest: sourceManifestDigest,
			ProviderID: providerID, TargetID: targetID, ContentPolicy: contentPolicy,
			DATVersionID: dbexec.StringPointer(datID), ValidationInputDigest: digest,
			BIOSDependencyDigest: biosDependencyDigest,
		},
	)
	if err != nil {
		return "", false, fmt.Errorf("schedule variant validation: %w", err)
	}
	return result.JobID, result.Queued, nil
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
`+contentquery.BindingPolicySQL+`
FROM games game
JOIN game_variants variant ON variant.id=? AND variant.game_id=game.id
JOIN runtime_target_bindings binding ON binding.core_id=variant.core_id
 AND binding.provider_id=? AND binding.target_id=? AND binding.launch_policy!='DISABLED'
LIMIT 1
`, item.variantID, providerID, targetID).Scan(
		&logicalName, &contentKind, &gameVersion, &sourceManifestDigest, contentquery.ScanPolicy(&contentPolicy),
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
		if _, err := recordstore.UpdateGameVariants(ctx, transaction, recordstore.Update{
			Set: `
dat_version_id=?,status='BLOCKED',compatibility_code='VALIDATION_PENDING',
emulator_game_id=NULL,version=version+1,updated_at_ms=?
`,
			Scope: recordstore.Scope{
				Where: `id=?`,
				Args:  []any{item.variantID},
			},
			Values: []any{targetDAT.String, service.now().UnixMilli()},
		}); err != nil {
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
