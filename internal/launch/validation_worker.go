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
	"retrom/internal/contentcapability"
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

// One readiness decision spans every validation input.
func (service *Service) validateVariant(
	parent context.Context,
	jobID, variantID, contentID, artifactID string,
	datID sql.NullString,
	digest string,
	biosDependencyDigest string,
) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	completed := false
	defer func() {
		if !completed {
			service.failValidationJob(parent, jobID, variantID)
		}
	}()
	if !service.startValidationJob(ctx, jobID, variantID) {
		return
	}
	outcome, err := service.loadValidationOutcome(
		ctx, variantID, contentID, artifactID, datID, digest, biosDependencyDigest,
	)
	if err != nil {
		return
	}
	if err := service.persistValidationOutcome(
		ctx, jobID, variantID, contentID, artifactID, digest, datID, outcome,
	); err != nil {
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
	variantID, contentID, artifactID string,
	datID sql.NullString,
	digest, biosDependencyDigest string,
) (variantValidationOutcome, error) {
	var contentLogicalName, contentKind string
	if err := service.database.QueryRowContext(ctx, `
SELECT COALESCE((SELECT logical_name FROM game_content_files
WHERE game_content_revision_id=r.id AND role IN ('CONTENT','DISC')
ORDER BY CASE role WHEN 'CONTENT' THEN 0 ELSE 1 END,sort_order,logical_name LIMIT 1),''),
r.content_kind FROM game_content_revisions r WHERE r.id=?
`, contentID).Scan(&contentLogicalName, &contentKind); err != nil {
		return variantValidationOutcome{}, fmt.Errorf("load validation content: %w", err)
	}
	currentDigest, currentBIOSDigest, biosSnapshot, biosStatus, biosCode, err := service.currentValidationEvidence(
		ctx, variantID, contentID, contentLogicalName, contentKind, artifactID, datID,
	)
	if err != nil {
		return variantValidationOutcome{}, fmt.Errorf("load validation evidence: %w", err)
	}
	biosSnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		return variantValidationOutcome{}, fmt.Errorf("encode validation BIOS snapshot: %w", err)
	}
	dependencySnapshotJSON := string(biosSnapshotJSON)
	if datID.Valid {
		dependencySnapshotJSON, err = service.lockedArcadeDependencySnapshot(
			ctx, variantID, contentID, contentLogicalName, datID.String,
		)
		if err != nil {
			return variantValidationOutcome{}, err
		}
	}
	status, code := service.validateContentForArtifact(ctx, contentID, artifactID, datID)
	if biosStatus != "READY" {
		status, code = biosStatus, biosCode
	}
	if currentDigest != digest || currentBIOSDigest != biosDependencyDigest {
		status, code = "BLOCKED", "LAUNCH_VALIDATION_INPUT_STALE"
	}
	return variantValidationOutcome{status, code, dependencySnapshotJSON, biosSnapshot}, nil
}

func (service *Service) persistValidationOutcome(
	ctx context.Context,
	jobID, variantID, contentID, artifactID, digest string,
	datID sql.NullString,
	outcome variantValidationOutcome,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin validation result transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	revisionID := newUUID()
	defaultDOSEntry, emulatorGameID, err := service.validationRevisionDefaults(
		ctx, transaction, variantID, contentID, outcome.status,
	)
	if err != nil {
		return err
	}
	if err := service.insertValidationRevision(
		ctx, transaction, revisionID, variantID, contentID, artifactID, digest, datID,
		defaultDOSEntry, emulatorGameID, outcome,
	); err != nil {
		return err
	}
	if err := service.copyValidationDependencies(
		ctx, transaction, revisionID, variantID, contentID, datID, outcome.biosSnapshot,
	); err != nil {
		return err
	}
	if err := service.finishValidationJob(ctx, transaction, jobID, variantID, revisionID, outcome); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit validation result: %w", err)
	}
	return nil
}

func (service *Service) validationRevisionDefaults(
	ctx context.Context,
	transaction *sql.Tx,
	variantID, contentID, status string,
) (sql.NullString, any, error) {
	var defaultDOSEntry sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(
  (SELECT r.default_dos_entry FROM game_variants v
   JOIN game_variant_revisions r ON r.id=v.current_revision_id
   WHERE v.id=? AND r.game_content_revision_id=?),
  (SELECT d.original_relative_path FROM dos_entries d
   WHERE d.game_content_revision_id=? AND d.enabled=1 AND d.direct_launch_safe=1
   ORDER BY d.rank,d.normalized_path LIMIT 1))
`, variantID, contentID, contentID).Scan(&defaultDOSEntry); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, nil, fmt.Errorf("load validation DOS entry: %w", err)
	}
	if status != "READY" {
		return defaultDOSEntry, nil, nil
	}
	var emulatorGameID int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(emulator_game_id),1000)+1 FROM game_variant_revisions
`).Scan(&emulatorGameID); err != nil {
		return sql.NullString{}, nil, fmt.Errorf("allocate emulator game ID: %w", err)
	}
	return defaultDOSEntry, emulatorGameID, nil
}

func (service *Service) insertValidationRevision(
	ctx context.Context,
	transaction *sql.Tx,
	revisionID, variantID, contentID, artifactID, digest string,
	datID sql.NullString,
	defaultDOSEntry sql.NullString,
	emulatorGameID any,
	outcome variantValidationOutcome,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,
dat_version_id,validation_input_digest,emulator_game_id,status,compatibility_code,
dependency_snapshot_json,default_dos_entry,created_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
`, revisionID, variantID, contentID, artifactID, nullableSQL(datID), digest, emulatorGameID,
		outcome.status, outcome.code, outcome.dependencySnapshotJSON, nullableSQL(defaultDOSEntry),
		service.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("insert validation revision: %w", err)
	}
	return nil
}

func (service *Service) copyValidationDependencies(
	ctx context.Context,
	transaction *sql.Tx,
	revisionID, variantID, contentID string,
	datID sql.NullString,
	biosSnapshot corevalidation.Snapshot,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
SELECT ?,vf.role,vf.logical_name,vf.blob_id,vf.sort_order FROM game_variants v
JOIN game_variant_revisions source ON source.id=v.current_revision_id
AND source.game_content_revision_id=? AND source.dat_version_id IS ?
JOIN variant_files vf ON vf.game_variant_revision_id=source.id
AND vf.role IN ('DOS_LAUNCH_BUNDLE','PARENT','MULTI_DISC_PLAYLIST') WHERE v.id=?
`, revisionID, contentID, nullableSQL(datID), variantID); err != nil {
		return fmt.Errorf("copy validation files: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_dependencies(game_variant_revision_id,kind,logical_archive,dat_version_id,
source_machine_name,required_entries_json,state,created_at_ms)
SELECT ?,dependency.kind,dependency.logical_archive,dependency.dat_version_id,
dependency.source_machine_name,dependency.required_entries_json,dependency.state,?
FROM game_variants variant JOIN game_variant_revisions source ON source.id=variant.current_revision_id
AND source.game_content_revision_id=? AND source.dat_version_id IS ?
JOIN variant_dependencies dependency ON dependency.game_variant_revision_id=source.id
WHERE variant.id=?
`, revisionID, service.now().UnixMilli(), contentID, nullableSQL(datID), variantID); err != nil {
		return fmt.Errorf("copy validation dependencies: %w", err)
	}
	for sortOrder, dependency := range biosSnapshot.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'BIOS_BUNDLE',?,?,?)
`, revisionID, dependency.LogicalName, *dependency.BlobID, sortOrder); err != nil {
			return fmt.Errorf("insert validation BIOS file: %w", err)
		}
	}
	return nil
}

func (service *Service) finishValidationJob(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, variantID, revisionID string,
	outcome variantValidationOutcome,
) error {
	jobState, eventType := "FAILED", "FAILED"
	var errorCode any = outcome.code
	var retryable any = 0
	if outcome.status == "READY" {
		jobState, eventType, errorCode, retryable = "SUCCEEDED", "SUCCEEDED", nil, nil
		if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants SET current_revision_id=?,version=version+1,updated_at_ms=? WHERE id=?
`, revisionID, service.now().UnixMilli(), variantID); err != nil {
			return fmt.Errorf("publish validation revision: %w", err)
		}
	}
	finished := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state=?,error_code=?,error_retryable=?,finished_at_ms=?,leased_until_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=?
`, jobState, errorCode, retryable, finished, finished, jobID); err != nil {
		return fmt.Errorf("finish validation job: %w", err)
	}
	data, _ := json.Marshal(map[string]any{"code": outcome.code, "variantRevisionId": revisionID})
	_, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,?,?,?)
`, jobID, variantID, eventType, string(data), finished)
	if err != nil {
		return fmt.Errorf("record validation completion: %w", err)
	}
	return nil
}

func (service *Service) failValidationJob(parent context.Context, jobID, variantID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
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
WHERE id=?
AND state='RUNNING';
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,'GAME_VARIANT',?,'FAILED',json_object('code','LAUNCH_CORE_VALIDATION_UNAVAILABLE'),?
FROM jobs
WHERE id=?
AND state='FAILED'
AND finished_at_ms=?
`, now, now, jobID, variantID, now, jobID, now)
}

func (service *Service) validateContentForArtifact(
	ctx context.Context,
	contentID, artifactID string,
	datID sql.NullString,
) (string, string) {
	var coreID, platformID, logicalName, contentKind, compatibilityJSON string
	var relationshipEnabled int
	err := service.database.QueryRowContext(ctx, `
SELECT a.core_id,
pi.platform_id,
COALESCE((SELECT f.logical_name
FROM game_content_files f
WHERE f.game_content_revision_id=cr.id
AND f.role='CONTENT'
ORDER BY f.sort_order,f.logical_name
LIMIT 1),''),
cr.content_kind,
a.compatibility_config_json,
EXISTS(SELECT 1
FROM platform_cores pc
WHERE pc.platform_id=pi.platform_id
AND pc.core_id=a.core_id
AND pc.enabled=1)
FROM core_artifacts a
JOIN game_content_revisions cr ON cr.id=?
JOIN games g ON g.id=cr.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
WHERE a.id=?
`, contentID, artifactID).
		Scan(&coreID, &platformID, &logicalName, &contentKind, &compatibilityJSON, &relationshipEnabled)
	if err != nil {
		return "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE"
	}
	if relationshipEnabled != 1 {
		return "INCOMPATIBLE", "CORE_PLATFORM_UNSUPPORTED"
	}
	if status, code := validateContentProfile(platformID, logicalName, contentKind, compatibilityJSON); status != "READY" {
		return status, code
	}
	if status, code := service.validateStaticBIOSForContent(ctx, artifactID, logicalName); status != "READY" {
		return status, code
	}
	if arcadedat.SupportsCore(coreID) {
		return service.validateArcadeContent(ctx, datID, logicalName)
	}
	return "READY", "READY"
}

func validateContentProfile(platformID, logicalName, contentKind, compatibilityJSON string) (string, string) {
	if contentKind == corevalidation.MultiDiscContentKind &&
		(!contentprofile.AllowsContentKind(platformID, contentprofile.ContentKindMultiDiscM3UV1) ||
			!contentcapability.SupportsContentKind(compatibilityJSON, contentKind)) {
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
SELECT classification
FROM dat_machines
WHERE dat_version_id=?
AND lower(machine_name)=lower(?)
`, datID.String, machine).Scan(&classification); err != nil ||
		classification != "NORMAL" {
		return "INCOMPATIBLE", "ARCADE_MACHINE_NOT_FOUND"
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
