package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
	"retrom/internal/tagging"
)

type approvalMetadata struct {
	Title, Description, Developer, Publisher, Genre string
	Players, ReleaseYear                            *int
}

type approvalRun struct {
	service                       *Service
	ctx                           context.Context
	transaction                   *sql.Tx
	itemID                        string
	expectedVersion               int64
	input                         approvalInput
	options                       approvalOptions
	draftID                       string
	state                         string
	importID                      string
	configJSON                    string
	platformID                    string
	platformInstanceID            string
	validationID                  string
	validationStatus              string
	metadataJSON                  string
	sourceSnapshotID              string
	sourceManifestJSON            string
	sourceManifestDigest          string
	contentKind                   string
	dependencySnapshotJSON        string
	coreID                        string
	artifactID                    string
	artifactCompatibility         string
	draftVersion                  int64
	artifactVersion               int64
	datID                         sql.NullString
	validationDOSEntry            sql.NullString
	draftDOSEntry                 sql.NullString
	candidateID                   sql.NullString
	coverID                       sql.NullString
	uploadedCoverID               sql.NullString
	backgroundID                  sql.NullString
	approvalScreenshotID          sql.NullString
	metadata                      approvalMetadata
	validationSnapshot            corevalidation.Snapshot
	snapshotValid                 bool
	screenshotOverride            bool
	runtimeDependencySnapshotJSON string
	contentIdentityDigest         string
	duplicateGames                []DuplicateGame
	now                           int64
	gameID                        string
	metadataID                    string
	contentID                     string
	variantID                     string
	variantRevisionID             string
	eventID                       string
	publishedTags                 []tagging.Reference
	screenshotIDs                 []string
}

func newApprovalRun(
	ctx context.Context,
	service *Service,
	transaction *sql.Tx,
	itemID string,
	expectedVersion int64,
	input approvalInput,
	options approvalOptions,
) *approvalRun {
	return &approvalRun{
		service: service, ctx: ctx, transaction: transaction, itemID: itemID,
		expectedVersion: expectedVersion, input: input, options: options,
	}
}

func (run *approvalRun) execute() error {
	if _, err := run.transaction.ExecContext(run.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	steps := []func() error{
		run.load, run.prepare, run.persistGame, run.persistVariant, run.persistDecision,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	approved := run.approved()
	if run.options.beforeCommit != nil {
		if err := run.options.beforeCommit(run.ctx, run.transaction, approved); err != nil {
			return err
		}
	}
	if err := run.transaction.Commit(); err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return nil
}

func (run *approvalRun) approved() Approved {
	return Approved{GameID: run.gameID, EventID: run.eventID, Status: "PUBLISHED"}
}

func (run *approvalRun) load() error {
	err := run.transaction.QueryRowContext(run.ctx, approvalDraftQuery, run.itemID).Scan(
		&run.draftID, &run.state, &run.importID, &run.configJSON, &run.platformID,
		&run.platformInstanceID, &run.validationID, &run.validationStatus, &run.metadataJSON,
		&run.sourceSnapshotID, &run.sourceManifestJSON, &run.sourceManifestDigest,
		&run.contentKind, &run.coreID, &run.artifactID, &run.artifactCompatibility,
		&run.artifactVersion, &run.datID, &run.validationDOSEntry, &run.draftDOSEntry,
		&run.dependencySnapshotJSON, &run.approvalScreenshotID, &run.draftVersion,
		&run.candidateID, &run.coverID, &run.uploadedCoverID, &run.backgroundID,
	)
	if err != nil || run.state != "REVIEW_PENDING" || run.draftVersion != run.expectedVersion {
		return ErrInvalid
	}
	if run.options.strictReady && run.validationStatus != "READY" {
		return ErrInvalid
	}
	if run.options.expectedValidationID != "" && run.validationID != run.options.expectedValidationID {
		return ErrInvalid
	}
	if run.options.expectedSourceSnapshotID != "" && run.sourceSnapshotID != run.options.expectedSourceSnapshotID {
		return ErrInvalid
	}
	return nil
}

func (run *approvalRun) prepare() error {
	if err := run.resolveServerOrigin(); err != nil {
		return err
	}
	if !contentcapability.SupportsContentKind(run.artifactCompatibility, run.contentKind) {
		return ErrInvalid
	}
	if err := run.prepareValidationSnapshot(); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(run.metadataJSON), &run.metadata); err != nil ||
		strings.TrimSpace(run.metadata.Title) == "" {
		return ErrInvalid
	}
	run.now = run.service.now().UnixMilli()
	if err := run.prepareDuplicateDecision(); err != nil {
		return err
	}
	run.allocateIDs()
	return nil
}

func (run *approvalRun) resolveServerOrigin() error {
	if run.input.decision.SourceKind != "" {
		return nil
	}
	origin, found, err := loadServerReviewOrigin(
		run.ctx, run.transaction, run.itemID, run.coverID.Valid || run.uploadedCoverID.Valid,
	)
	if err != nil {
		return err
	}
	if found {
		run.input.metadataSourceKind = "SERVER_PEGASUS_IMPORT"
		run.input.metadataSourceRefID = origin.SourceRefID
		run.input.decision.ExternalAssets = origin.Assets
	}
	return nil
}

func (run *approvalRun) prepareValidationSnapshot() error {
	run.screenshotOverride = run.validationStatus != "READY" && run.approvalScreenshotID.Valid
	snapshot, err := corevalidation.ParseSnapshot(run.dependencySnapshotJSON)
	run.validationSnapshot, run.snapshotValid = snapshot, err == nil
	if !run.screenshotOverride {
		if err := run.service.validateCurrentApprovalDependencySnapshot(
			run.ctx, run.transaction, run.sourceSnapshotID, run.validationID, run.platformID,
			run.artifactID, run.artifactCompatibility, run.contentKind, run.dependencySnapshotJSON,
		); err != nil {
			return err
		}
	}
	run.runtimeDependencySnapshotJSON = run.dependencySnapshotJSON
	if !run.snapshotValid || !run.screenshotOverride {
		return nil
	}
	run.validationSnapshot = screenshotOverrideRuntimeSnapshot(run.validationSnapshot)
	encoded, err := run.validationSnapshot.JSON()
	if err != nil {
		return ErrInvalid
	}
	run.runtimeDependencySnapshotJSON = string(encoded)
	return nil
}

func (run *approvalRun) prepareDuplicateDecision() error {
	digest, err := importItemContentIdentity(run.ctx, run.transaction, run.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: publish draft tags: %w", err)
	}
	run.contentIdentityDigest = digest
	if err := claimContentIdentity(run.ctx, run.transaction, run.platformID, digest, run.now); err != nil {
		return err
	}
	run.duplicateGames, err = findDuplicateGames(run.ctx, run.transaction, run.itemID, run.platformID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: publish draft tags: %w", err)
	}
	decision := run.input.decision
	if len(run.duplicateGames) > 0 &&
		(decision.DuplicatePolicy != "ALLOW_NEW" ||
			!sameDuplicateIDs(run.duplicateGames, decision.AcknowledgedGameIDs)) {
		return &DuplicateConflict{ContentIdentityDigest: digest, Games: run.duplicateGames}
	}
	if len(run.duplicateGames) == 0 && decision.DuplicatePolicy != "" {
		return ErrInvalid
	}
	return nil
}

func (run *approvalRun) allocateIDs() {
	gameID, _ := uuid.NewV7()
	metadataID, _ := uuid.NewV7()
	contentID, _ := uuid.NewV7()
	variantID, _ := uuid.NewV7()
	variantRevisionID, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()
	run.gameID, run.metadataID, run.contentID = gameID.String(), metadataID.String(), contentID.String()
	run.variantID = variantID.String()
	run.variantRevisionID = variantRevisionID.String()
	run.eventID = eventID.String()
}

const approvalDraftQuery = `
SELECT d.id,i.state,i.import_job_id,j.config_snapshot_json,p.platform_id,
  d.target_platform_instance_id,v.id,v.status,d.metadata_json,source_snapshot.id,
  source_snapshot.source_manifest_json,source_snapshot.source_manifest_digest,
  source_snapshot.content_kind,v.core_id,v.core_artifact_id,a.compatibility_config_json,
  v.core_artifact_version,v.dat_version_id,v.default_dos_entry,d.default_dos_entry,
  v.dependency_snapshot_json,
  (SELECT screenshot.id FROM review_runtime_screenshots screenshot
   WHERE screenshot.import_item_id=i.id AND screenshot.validation_id=v.id
     AND screenshot.source_snapshot_id=d.effective_source_snapshot_id
     AND screenshot.core_artifact_id=v.core_artifact_id
   ORDER BY screenshot.captured_at_ms DESC,screenshot.id DESC LIMIT 1),
  d.version,d.selected_candidate_id,d.cover_candidate_asset_id,d.cover_uploaded_asset_id,
  d.background_candidate_asset_id
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=d.effective_source_snapshot_id
JOIN platform_instances p ON p.id=d.target_platform_instance_id
  AND p.enabled=1 AND p.deleted_at_ms IS NULL
JOIN core_artifacts a ON a.core_id=p.default_core_id AND a.enabled=1
JOIN import_item_core_validations v ON v.id=(
  SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=i.id
    AND candidate.source_snapshot_id=d.effective_source_snapshot_id
    AND candidate.target_platform_instance_id=d.target_platform_instance_id
    AND candidate.core_artifact_id=a.id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
AND v.source_snapshot_id=d.effective_source_snapshot_id
AND v.target_platform_instance_id=d.target_platform_instance_id
AND v.core_artifact_id=a.id
AND p.version=v.platform_instance_version
AND a.version=v.core_artifact_version
WHERE i.id=?
AND (v.status='READY' OR EXISTS(
  SELECT 1 FROM review_runtime_screenshots screenshot
  WHERE screenshot.import_item_id=i.id AND screenshot.validation_id=v.id
    AND screenshot.source_snapshot_id=d.effective_source_snapshot_id
    AND screenshot.core_artifact_id=v.core_artifact_id
))
AND v.prepublish_generation=4
AND v.default_dos_entry IS d.default_dos_entry
AND v.dat_version_id IS (
  SELECT active.id FROM dat_versions active
  WHERE active.core_artifact_id=a.id AND active.is_active=1
)
`
