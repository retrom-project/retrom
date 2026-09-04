package launch

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
)

func (service *Service) CreateRPGValidation(
	ctx context.Context,
	profileID, validationID, returnTo string,
	capabilities Capabilities,
) (Created, error) {
	return service.createRPGValidationLaunch(
		ctx, profileID, validationID, returnTo, capabilities, false,
	)
}

func (service *Service) CreateRPGValidationRestore(
	ctx context.Context,
	profileID, validationID, returnTo string,
	capabilities Capabilities,
) (Created, error) {
	return service.createRPGValidationLaunch(
		ctx, profileID, validationID, returnTo, capabilities, true,
	)
}

type rpgValidationBinding struct {
	itemID, sourceSnapshotID, providerID, targetID string
	bundleSHA256, coreID, contentKind              string
	dependencySnapshot, compatibilityCode          string
	deliveryProfile                                string
	launchID, restoreLaunchID                      sql.NullString
	state                                          string
	expiresAt                                      int64
	requiresThreads                                bool
}

func (service *Service) createRPGValidationLaunch(
	ctx context.Context,
	profileID, validationID, returnTo string,
	capabilities Capabilities,
	restore bool,
) (Created, error) {
	if profileID == "" || validationID == "" {
		return Created{}, ErrBlocked
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("create RPG validation launch: %w", err)
	}
	defer cleanup.Rollback(transaction)
	binding, err := loadRPGValidationBinding(ctx, transaction, validationID)
	if err != nil || returnTo != "/admin/reviews/"+binding.itemID ||
		!validThreadCapabilities(binding.requiresThreads, capabilities) {
		return Created{}, ErrBlocked
	}
	existingID := binding.launchID
	if restore {
		existingID = binding.restoreLaunchID
	}
	if existingID.Valid {
		return service.existingRPGValidationLaunch(ctx, transaction, existingID.String)
	}
	now := service.now().UnixMilli()
	if binding.expiresAt <= now || !validRPGValidationCreateState(
		ctx, transaction, validationID, binding, restore,
	) {
		return Created{}, ErrBlocked
	}
	var content launchContentPlan
	if restore {
		content, err = copyRPGValidationContentPlan(ctx, transaction, binding.launchID.String)
	} else {
		content, err = service.buildRPGValidationContentPlan(
			ctx, transaction, validationID, binding.sourceSnapshotID, binding.deliveryProfile,
		)
	}
	if err != nil {
		return Created{}, err
	}
	created, err := service.insertRPGValidationLaunch(
		ctx, transaction, profileID, validationID, returnTo, binding, content, now, restore,
	)
	if err != nil {
		return Created{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("commit RPG validation launch: %w", err)
	}
	return created, nil
}

func loadRPGValidationBinding(
	ctx context.Context,
	transaction *sql.Tx,
	validationID string,
) (rpgValidationBinding, error) {
	var binding rpgValidationBinding
	err := transaction.QueryRowContext(ctx, `
SELECT validation.import_item_id,validation.effective_source_snapshot_id,
validation.provider_id,validation.target_id,provider.bundle_sha256,
core_validation.core_id,snapshot.content_kind,core_validation.dependency_snapshot_json,
core_validation.compatibility_code,binding.delivery_profile,
validation.launch_id,validation.restore_launch_id,validation.state,validation.expires_at_ms,
json_extract(target.capabilities_json,'$.requiresThreads')
FROM rpgmaker_runtime_validations validation
JOIN import_item_source_snapshots snapshot ON snapshot.id=validation.effective_source_snapshot_id
JOIN import_item_core_validations core_validation ON core_validation.id=(
 SELECT candidate.id FROM import_item_core_validations candidate
 WHERE candidate.import_item_id=validation.import_item_id
  AND candidate.source_snapshot_id=validation.effective_source_snapshot_id
  AND candidate.provider_id=validation.provider_id AND candidate.target_id=validation.target_id
 ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
JOIN runtime_targets target ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id
 AND binding.target_id=target.target_id AND binding.core_id=core_validation.core_id
WHERE validation.id=? AND binding.review_policy='RPG_RUNTIME_VALIDATION'
 AND binding.launch_policy!='DISABLED'
`, validationID).Scan(
		&binding.itemID, &binding.sourceSnapshotID, &binding.providerID, &binding.targetID,
		&binding.bundleSHA256, &binding.coreID, &binding.contentKind,
		&binding.dependencySnapshot, &binding.compatibilityCode, &binding.deliveryProfile,
		&binding.launchID, &binding.restoreLaunchID, &binding.state, &binding.expiresAt,
		&binding.requiresThreads,
	)
	if err != nil {
		return rpgValidationBinding{}, fmt.Errorf("load RPG validation binding: %w", err)
	}
	return binding, nil
}

func validRPGValidationCreateState(
	ctx context.Context,
	transaction *sql.Tx,
	validationID string,
	binding rpgValidationBinding,
	restore bool,
) bool {
	if !restore {
		return binding.state == "CREATED" && !binding.launchID.Valid
	}
	if binding.state != "CHECKPOINTED" || !binding.launchID.Valid || binding.restoreLaunchID.Valid {
		return false
	}
	var valid int
	_ = transaction.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM rpgmaker_runtime_validation_checkpoints checkpoint
  JOIN launch_sessions original ON original.id=?
  WHERE checkpoint.validation_id=?
    AND original.state IN ('FINISHED','EXPIRED','REVOKED')
)
`, binding.launchID.String, validationID).Scan(&valid)
	return valid == 1
}

func (service *Service) existingRPGValidationLaunch(
	ctx context.Context,
	transaction *sql.Tx,
	launchID string,
) (Created, error) {
	var state string
	var bootstrapExpires, hardExpires int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,bootstrap_expires_at_ms,hard_expires_at_ms
FROM launch_sessions WHERE id=? AND purpose='RPG_RUNTIME_VALIDATION'
`, launchID).Scan(&state, &bootstrapExpires, &hardExpires); err != nil ||
		state != "CREATED" && state != "ACTIVE" || hardExpires <= service.now().UnixMilli() {
		return Created{}, ErrBlocked
	}
	parsed, err := uuid.Parse(launchID)
	if err != nil {
		return Created{}, ErrBlocked
	}
	return Created{
		LaunchID: launchID, PlayURL: "/play/" + launchID, Warnings: []string{},
		BootstrapExpiresAtMS: bootstrapExpires, HardExpiresAtMS: hardExpires,
		Capability: retromruntime.EncodeCapability(service.credentials.Capability(parsed)), Existing: true,
	}, nil
}

func (service *Service) insertRPGValidationLaunch(
	ctx context.Context,
	transaction *sql.Tx,
	profileID, validationID, returnTo string,
	binding rpgValidationBinding,
	content launchContentPlan,
	now int64,
	restore bool,
) (Created, error) {
	launchID, err := uuid.NewV7()
	if err != nil {
		return Created{}, fmt.Errorf("generate RPG validation launch: %w", err)
	}
	capability := service.credentials.Capability(launchID)
	credentialHash := retromruntime.HashCapability(capability)
	bootstrapExpires := min(binding.expiresAt, now+int64(5*time.Minute/time.Millisecond))
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_sessions(
  id,profile_id,purpose,game_id,core_id,provider_id,target_id,bundle_sha256,
  content_kind,dependency_snapshot_json,compatibility_code,
  effective_source_snapshot_id,rpgmaker_runtime_validation_id,
  save_state_id,dos_entry_path,initial_disc_index,return_to,credential_sha256,state,
  bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,'RPG_RUNTIME_VALIDATION',NULL,?,?,?,?,?,?,?,?,?,NULL,NULL,0,?,?,'CREATED',?,?,?,?)
`, launchID.String(), profileID, binding.coreID, binding.providerID, binding.targetID,
		binding.bundleSHA256, binding.contentKind, binding.dependencySnapshot, binding.compatibilityCode,
		binding.sourceSnapshotID,
		validationID, returnTo, credentialHash[:], bootstrapExpires, binding.expiresAt, now, now); err != nil {
		return Created{}, fmt.Errorf("insert RPG validation launch: %w", err)
	}
	if binding.deliveryProfile == "ISOLATED_WEB_PROJECT" {
		if err := service.lockIsolatedLaunchBootstrapTicket(
			ctx, transaction, launchID.String(), profileID, now,
		); err != nil {
			return Created{}, err
		}
	}
	if err := lockLaunchContentFiles(ctx, transaction, launchID.String(), content.Files, now); err != nil {
		return Created{}, err
	}
	column := "launch_id"
	where := "state='CREATED' AND launch_id IS NULL"
	nextState := ",state='STARTING'"
	if restore {
		column = "restore_launch_id"
		where = "state='CHECKPOINTED' AND launch_id IS NOT NULL AND restore_launch_id IS NULL"
		nextState = ""
	}
	result, err := transaction.ExecContext(ctx, `UPDATE rpgmaker_runtime_validations SET `+column+`=?`+
		nextState+`,updated_at_ms=? WHERE id=? AND `+where, launchID.String(), now, validationID)
	if err != nil {
		return Created{}, fmt.Errorf("bind RPG validation launch: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Created{}, ErrBlocked
	}
	return Created{
		LaunchID: launchID.String(), PlayURL: "/play/" + launchID.String(), Warnings: []string{},
		BootstrapExpiresAtMS: bootstrapExpires, HardExpiresAtMS: binding.expiresAt,
		Capability: retromruntime.EncodeCapability(capability),
	}, nil
}

func lockLaunchContentFiles(
	ctx context.Context,
	transaction *sql.Tx,
	launchID string,
	files []lockedContentFile,
	now int64,
) error {
	for _, file := range files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES(?,?,?,?,?)
`, launchID, file.LogicalName, file.BlobID, file.Format, now); err != nil {
			return fmt.Errorf("lock RPG validation content: %w", err)
		}
	}
	return nil
}
