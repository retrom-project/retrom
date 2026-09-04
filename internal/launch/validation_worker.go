package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"retrom/internal/arcadedat"
	"retrom/internal/cleanup"
	"retrom/internal/contentprofile"
	"retrom/internal/corevalidation"

	"github.com/google/uuid"
)

type variantValidationOutcome struct {
	status                 string
	code                   string
	dependencySnapshotJSON string
	biosSnapshot           corevalidation.Snapshot
}

var errValidationGameChanged = errors.New("validation game changed")

func (service *Service) validateVariant(
	parent context.Context,
	jobID string,
	inputs validationInputs,
	datID sql.NullString,
) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	if !service.startValidationJob(ctx, jobID, inputs.GameVariantID) {
		return
	}
	completed := false
	defer func() {
		if !completed {
			service.failValidationJob(parent, jobID, inputs.GameVariantID)
		}
	}()
	outcome, err := service.loadValidationOutcome(ctx, inputs, datID)
	if errors.Is(err, errValidationGameChanged) {
		service.cancelValidationForChangedGame(parent, jobID, inputs.GameVariantID)
		completed = true
		return
	}
	if err != nil {
		return
	}
	if err := service.persistValidationOutcome(ctx, jobID, inputs, datID, outcome); err != nil {
		if errors.Is(err, errValidationGameChanged) {
			service.cancelValidationForChangedGame(parent, jobID, inputs.GameVariantID)
			completed = true
		}
		return
	}
	completed = true
}

func (service *Service) startValidationJob(ctx context.Context, jobID, variantID string) bool {
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',attempt_count=attempt_count+1,execution_started_at_ms=?,
execution_deadline_at_ms=?,leased_until_ms=?,heartbeat_at_ms=?,worker_id='in-process',
version=version+1,updated_at_ms=?
WHERE id=? AND state='QUEUED'
`, now, now+int64(30*time.Minute/time.Millisecond), now+60_000, now, now, jobID)
	if err != nil {
		return false
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return false
	}
	_, _ = service.database.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,'STARTED','{}',?)
`, jobID, variantID, now)
	return true
}

func (service *Service) loadValidationOutcome(
	ctx context.Context,
	inputs validationInputs,
	datID sql.NullString,
) (variantValidationOutcome, error) {
	var contentLogicalName, contentKind, sourceManifestDigest string
	var gameVersion int64
	if err := service.database.QueryRowContext(ctx, `
SELECT COALESCE((SELECT logical_name FROM game_files
 WHERE game_id=game.id AND role IN ('CONTENT','DISC')
 ORDER BY CASE role WHEN 'CONTENT' THEN 0 ELSE 1 END,sort_order,logical_name LIMIT 1),''),
game.content_kind,game.version,game.source_manifest_digest
FROM games game WHERE game.id=? AND game.status='PUBLISHED'
`, inputs.GameID).Scan(
		&contentLogicalName, &contentKind, &gameVersion, &sourceManifestDigest,
	); err != nil {
		return variantValidationOutcome{}, fmt.Errorf("load validation content: %w", err)
	}
	if gameVersion != inputs.GameVersion || sourceManifestDigest != inputs.SourceManifestDigest {
		return variantValidationOutcome{}, errValidationGameChanged
	}
	baseDigest, currentBIOSDigest, biosSnapshot, biosStatus, biosCode, err := service.currentValidationEvidence(
		ctx, inputs.GameVariantID, inputs.GameID, contentLogicalName, contentKind,
		inputs.ProviderID, inputs.TargetID, inputs.ContentPolicyJSON, datID,
	)
	if err != nil {
		return variantValidationOutcome{}, fmt.Errorf("load validation evidence: %w", err)
	}
	if bindCurrentGameStateDigest(baseDigest, gameVersion, sourceManifestDigest) != inputs.ValidationInputDigest ||
		currentBIOSDigest != inputs.BIOSDependencyDigest {
		return variantValidationOutcome{}, errValidationGameChanged
	}
	biosSnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		return variantValidationOutcome{}, fmt.Errorf("encode validation BIOS snapshot: %w", err)
	}
	dependencySnapshotJSON := string(biosSnapshotJSON)
	if datID.Valid {
		dependencySnapshotJSON, err = service.lockedArcadeDependencySnapshot(
			ctx, inputs.GameVariantID, inputs.GameID, contentLogicalName, datID.String,
		)
		if err != nil {
			return variantValidationOutcome{}, err
		}
	}
	status, code := service.validateContentForTarget(
		ctx, inputs.GameID, inputs.ProviderID, inputs.TargetID, datID,
	)
	if biosStatus != "READY" {
		status, code = biosStatus, biosCode
	}
	return variantValidationOutcome{status, code, dependencySnapshotJSON, biosSnapshot}, nil
}

func (service *Service) persistValidationOutcome(
	ctx context.Context,
	jobID string,
	inputs validationInputs,
	datID sql.NullString,
	outcome variantValidationOutcome,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin validation result transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var currentVersion int64
	var currentManifest string
	if err := transaction.QueryRowContext(ctx, `
SELECT version,source_manifest_digest FROM games WHERE id=? AND status='PUBLISHED'
`, inputs.GameID).Scan(&currentVersion, &currentManifest); err != nil {
		return fmt.Errorf("load validation game: %w", err)
	}
	if currentVersion != inputs.GameVersion || currentManifest != inputs.SourceManifestDigest {
		return errValidationGameChanged
	}
	defaultDOSEntry, emulatorGameID, err := service.validationDefaults(
		ctx, transaction, inputs.GameVariantID, inputs.GameID, outcome.status,
	)
	if err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET provider_id=?,target_id=?,dat_version_id=?,emulator_game_id=?,status=?,compatibility_code=?,
dependency_snapshot_json=?,default_dos_entry=?,version=version+1,updated_at_ms=?
WHERE id=? AND game_id=?
`, inputs.ProviderID, inputs.TargetID, nullableSQL(datID), emulatorGameID,
		outcome.status, outcome.code, outcome.dependencySnapshotJSON, nullableSQL(defaultDOSEntry),
		service.now().UnixMilli(), inputs.GameVariantID, inputs.GameID)
	if err != nil {
		return fmt.Errorf("update current game variant: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errValidationGameChanged
	}
	if err := service.replaceValidationBIOSFiles(
		ctx, transaction, inputs.GameVariantID, outcome.biosSnapshot,
	); err != nil {
		return err
	}
	if err := service.finishValidationJob(ctx, transaction, jobID, inputs.GameVariantID, outcome); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit validation result: %w", err)
	}
	return nil
}

func (service *Service) validationDefaults(
	ctx context.Context,
	transaction *sql.Tx,
	variantID, gameID, status string,
) (sql.NullString, any, error) {
	var defaultDOSEntry sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(
 (SELECT default_dos_entry FROM game_variants WHERE id=? AND game_id=?),
 (SELECT original_relative_path FROM dos_entries
  WHERE game_id=? AND enabled=1 AND direct_launch_safe=1
  ORDER BY rank,normalized_path LIMIT 1))
`, variantID, gameID, gameID).Scan(&defaultDOSEntry); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, nil, fmt.Errorf("load validation DOS entry: %w", err)
	}
	if status != "READY" {
		return defaultDOSEntry, nil, nil
	}
	var existing sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT emulator_game_id FROM game_variants WHERE id=?
`, variantID).Scan(&existing); err != nil {
		return sql.NullString{}, nil, fmt.Errorf("load emulator game ID: %w", err)
	}
	if existing.Valid {
		return defaultDOSEntry, existing.Int64, nil
	}
	var emulatorGameID int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variants
`).Scan(&emulatorGameID); err != nil {
		return sql.NullString{}, nil, fmt.Errorf("allocate emulator game ID: %w", err)
	}
	return defaultDOSEntry, emulatorGameID, nil
}

func (service *Service) replaceValidationBIOSFiles(
	ctx context.Context,
	transaction *sql.Tx,
	variantID string,
	biosSnapshot corevalidation.Snapshot,
) error {
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM variant_files WHERE game_variant_id=? AND role='BIOS_BUNDLE'
`, variantID); err != nil {
		return fmt.Errorf("delete current validation BIOS files: %w", err)
	}
	for sortOrder, dependency := range biosSnapshot.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
VALUES(?,'BIOS_BUNDLE',?,?,?)
`, variantID, dependency.LogicalName, *dependency.BlobID, sortOrder); err != nil {
			return fmt.Errorf("insert current validation BIOS file: %w", err)
		}
	}
	return nil
}

func (service *Service) finishValidationJob(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, variantID string,
	outcome variantValidationOutcome,
) error {
	jobState, eventType := "FAILED", "FAILED"
	var errorCode any = outcome.code
	var retryable any = 0
	if outcome.status == "READY" {
		jobState, eventType, errorCode, retryable = "SUCCEEDED", "SUCCEEDED", nil, nil
	}
	finished := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state=?,error_code=?,error_retryable=?,finished_at_ms=?,leased_until_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=?
`, jobState, errorCode, retryable, finished, finished, jobID); err != nil {
		return fmt.Errorf("finish validation job: %w", err)
	}
	data, _ := json.Marshal(map[string]any{"code": outcome.code, "gameVariantId": variantID})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,?,?,?)
`, jobID, variantID, eventType, string(data), finished); err != nil {
		return fmt.Errorf("record validation completion: %w", err)
	}
	return nil
}

func (service *Service) cancelValidationForChangedGame(parent context.Context, jobID, variantID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',error_code='GAME_STATE_CHANGED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING';
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,'GAME_VARIANT',?,'CANCELLED',json_object('code','GAME_STATE_CHANGED'),?
FROM jobs WHERE id=? AND state='CANCELLED' AND finished_at_ms=?
`, now, now, jobID, variantID, now, jobID, now)
}

func (service *Service) failValidationJob(parent context.Context, jobID, variantID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE jobs
SET state='FAILED',error_code='LAUNCH_CORE_VALIDATION_UNAVAILABLE',error_retryable=1,
finished_at_ms=?,leased_until_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING';
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,'GAME_VARIANT',?,'FAILED',json_object('code','LAUNCH_CORE_VALIDATION_UNAVAILABLE'),?
FROM jobs WHERE id=? AND state='FAILED' AND finished_at_ms=?
`, now, now, jobID, variantID, now, jobID, now)
}

func (service *Service) validateContentForTarget(
	ctx context.Context,
	gameID, providerID, targetID string,
	datID sql.NullString,
) (string, string) {
	var coreID, platformID, logicalName, contentKind string
	var relationshipEnabled int
	err := service.database.QueryRowContext(ctx, `
SELECT binding.core_id,
instance.platform_id,
COALESCE((SELECT logical_name FROM game_files
 WHERE game_id=game.id AND role='CONTENT'
 ORDER BY sort_order,logical_name LIMIT 1),''),
game.content_kind,
EXISTS(SELECT 1 FROM platform_cores platform_core
 WHERE platform_core.platform_id=instance.platform_id
 AND platform_core.core_id=binding.core_id AND platform_core.enabled=1)
FROM runtime_target_bindings binding
JOIN games game ON game.id=?
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id AND binding_platform.core_id=binding.core_id
JOIN runtime_binding_content_kinds binding_kind ON binding_kind.binding_id=binding.binding_id
 AND binding_kind.content_kind=game.content_kind
WHERE binding.provider_id=? AND binding.target_id=? AND binding.launch_policy!='DISABLED'
`, gameID, providerID, targetID).Scan(
		&coreID, &platformID, &logicalName, &contentKind, &relationshipEnabled,
	)
	if err != nil {
		return "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
	}
	if relationshipEnabled != 1 {
		return "INCOMPATIBLE", "CORE_PLATFORM_UNSUPPORTED"
	}
	if status, code := validateContentProfile(platformID, logicalName, contentKind); status != "READY" {
		return status, code
	}
	if status, code := service.validateStaticBIOSForContent(ctx, providerID, targetID, logicalName); status != "READY" {
		return status, code
	}
	if arcadedat.SupportsCore(coreID) {
		return service.validateArcadeContent(ctx, datID, logicalName)
	}
	return "READY", "READY"
}

func validateContentProfile(platformID, logicalName, contentKind string) (string, string) {
	if contentKind == corevalidation.MultiDiscContentKind &&
		!contentprofile.AllowsContentKind(platformID, contentprofile.ContentKindMultiDisc) {
		return "INCOMPATIBLE", "CORE_CONTENT_FORMAT_UNSUPPORTED"
	}
	if _, exists := contentprofile.ByPlatform(platformID); exists {
		if !contentprofile.AcceptsRaw(platformID, logicalName) {
			return "INCOMPATIBLE", "CORE_CONTENT_FORMAT_UNSUPPORTED"
		}
		return "READY", "READY"
	}
	if platformID != "arcade" && platformID != "dos" {
		return "BLOCKED", "CORE_CONTENT_PROFILE_MISSING"
	}
	return "READY", "READY"
}

func (service *Service) validateArcadeContent(
	ctx context.Context,
	datID sql.NullString,
	logicalName string,
) (string, string) {
	if !datID.Valid || !strings.EqualFold(filepath.Ext(logicalName), ".zip") {
		return "INCOMPATIBLE", "ARCADE_CONTENT_NOT_ROMSET"
	}
	machine := strings.TrimSuffix(filepath.Base(logicalName), filepath.Ext(logicalName))
	var classification string
	if err := service.database.QueryRowContext(ctx, `
SELECT classification FROM dat_machines
WHERE dat_version_id=? AND lower(machine_name)=lower(?)
`, datID.String, machine).Scan(&classification); err != nil || classification != "NORMAL" {
		return "INCOMPATIBLE", "ARCADE_MACHINE_NOT_FOUND"
	}
	return "READY", "READY"
}

func (service *Service) validateStaticBIOSForContent(
	ctx context.Context,
	providerID, targetID, logicalName string,
) (string, string) {
	_, status, code, err := corevalidation.ResolveBIOS(ctx, service.database, providerID, targetID, logicalName)
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
