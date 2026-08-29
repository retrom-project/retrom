package saves

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	retromruntime "retrom/internal/runtime"
)

type launchSnapshot struct {
	principalID, profileID, purpose, gameID, contentRevisionID string
	variantRevisionID, artifactID, validationID, generation    string
	runtimeFamily, adapterABI, saveABI, dependencySHA256       string
	payloadKind                                                string
	datVersionID, dosEntry                                     sql.NullString
	credentialHash                                             []byte
	state                                                      string
	hardExpiresAtMS, payloadMaxBytes                           int64
	contentFormat                                              string
	discCount, initialDiscIndex                                int
	originalValidationLaunch                                   bool
}

func (service *Service) launch(ctx context.Context, launchID, capability string) (launchSnapshot, error) {
	var result launchSnapshot
	var gameID, contentRevisionID, variantRevisionID, validationID sql.NullString
	var variantDependencyJSON, productGeneration, productABI, productDependency sql.NullString
	var validationGeneration, validationABI, validationDependency sql.NullString
	var validationLaunchID sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT COALESCE(user.id,launch.profile_id),launch.profile_id,launch.purpose,
 launch.game_id,launch.game_content_revision_id,launch.game_variant_revision_id,
 launch.core_artifact_id,launch.rpgmaker_runtime_validation_id,artifact.runtime_family,
 revision.dat_version_id,launch.dos_entry_path,launch.credential_sha256,launch.state,
 launch.hard_expires_at_ms,artifact.save_payload_kind,artifact.save_max_bytes,
 json_extract(artifact.compatibility_json,'$.adapterAbi'),
 COALESCE(json_extract(artifact.compatibility_json,'$.saveAbi'),
          json_extract(artifact.compatibility_json,'$.adapterAbi')),
 revision.dependency_snapshot_json,
 product_profile.generation,product_profile.adapter_abi,product_profile.dependency_snapshot_sha256,
 validation.generation,validation.adapter_abi,validation.dependency_snapshot_sha256,validation.launch_id,
 CASE WHEN EXISTS(SELECT 1 FROM launch_content_files file
       WHERE file.launch_session_id=launch.id AND file.format_version='RETROM_MULTIDISC_M3U_V1')
      THEN 'RETROM_MULTIDISC_M3U_V1'
      ELSE COALESCE((SELECT min(file.format_version) FROM launch_content_files file
                     WHERE file.launch_session_id=launch.id),'') END,
 (SELECT count(*) FROM launch_external_files external
  WHERE external.launch_session_id=launch.id AND external.kind='DISC'),
 launch.initial_disc_index
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
LEFT JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
LEFT JOIN rpgmaker_variant_profiles product_profile
  ON product_profile.game_variant_revision_id=launch.game_variant_revision_id
LEFT JOIN rpgmaker_runtime_validations validation
  ON validation.id=launch.rpgmaker_runtime_validation_id
LEFT JOIN users user ON user.profile_id=launch.profile_id
WHERE launch.id=?
`, launchID).Scan(
		&result.principalID, &result.profileID, &result.purpose,
		&gameID, &contentRevisionID, &variantRevisionID, &result.artifactID, &validationID,
		&result.runtimeFamily,
		&result.datVersionID, &result.dosEntry, &result.credentialHash, &result.state,
		&result.hardExpiresAtMS, &result.payloadKind, &result.payloadMaxBytes,
		&result.adapterABI, &result.saveABI, &variantDependencyJSON,
		&productGeneration, &productABI, &productDependency,
		&validationGeneration, &validationABI, &validationDependency, &validationLaunchID,
		&result.contentFormat, &result.discCount, &result.initialDiscIndex,
	)
	if !validLaunchAccess(err, capability, result, service.now().UnixMilli()) {
		return launchSnapshot{}, ErrCredential
	}
	result.gameID, result.contentRevisionID, result.variantRevisionID = gameID.String, contentRevisionID.String,
		variantRevisionID.String
	result.validationID = validationID.String
	if result.purpose == "PRODUCT" {
		return bindProductLaunch(
			result, gameID, contentRevisionID, variantRevisionID, variantDependencyJSON,
			productGeneration, productABI, productDependency,
		)
	}
	return bindValidationLaunch(
		result, validationID, validationGeneration, validationABI, validationDependency, validationLaunchID, launchID,
	)
}

func validLaunchAccess(err error, capability string, launch launchSnapshot, now int64) bool {
	return err == nil && retromruntime.MatchesCapability(capability, launch.credentialHash) &&
		launch.state == "ACTIVE" && launch.hardExpiresAtMS > now
}

func bindProductLaunch(
	result launchSnapshot,
	gameID, contentRevisionID, variantRevisionID, variantDependencyJSON sql.NullString,
	productGeneration, productABI, productDependency sql.NullString,
) (launchSnapshot, error) {
	if !gameID.Valid || !contentRevisionID.Valid || !variantRevisionID.Valid || !variantDependencyJSON.Valid ||
		!validLaunchDiscShape(result) {
		return launchSnapshot{}, ErrCredential
	}
	if !productGeneration.Valid {
		digest := sha256.Sum256([]byte(variantDependencyJSON.String))
		result.dependencySHA256 = hex.EncodeToString(digest[:])
		return result, nil
	}
	if !productABI.Valid || !productDependency.Valid {
		return launchSnapshot{}, ErrCredential
	}
	result.generation, result.dependencySHA256 = productGeneration.String, productDependency.String
	return result, nil
}

func bindValidationLaunch(
	result launchSnapshot,
	validationID, generation, adapterABI, dependency, originalLaunchID sql.NullString,
	launchID string,
) (launchSnapshot, error) {
	if result.purpose != "RPG_RUNTIME_VALIDATION" || !validationID.Valid || !generation.Valid ||
		!adapterABI.Valid || !dependency.Valid || !originalLaunchID.Valid {
		return launchSnapshot{}, ErrCredential
	}
	result.generation, result.adapterABI, result.dependencySHA256 = generation.String, adapterABI.String, dependency.String
	result.originalValidationLaunch = originalLaunchID.String == launchID
	return result, nil
}

func validLaunchDiscShape(result launchSnapshot) bool {
	if result.contentFormat != "RETROM_MULTIDISC_M3U_V1" {
		return result.discCount == 0 && result.initialDiscIndex == 0
	}
	return result.discCount >= 2 && result.initialDiscIndex >= 0 && result.initialDiscIndex < result.discCount
}

type restoreBinding struct {
	gameID, contentID, variantID, validationID                    sql.NullString
	productGeneration, productABI, productDependency              sql.NullString
	validationGeneration, validationABI, validationDependency     sql.NullString
	variantDependencyJSON                                         sql.NullString
	payloadDigest, savedAdapterABI, savedSaveABI, savedDependency string
	storedPayloadKind                                             string
	savedSize                                                     int64
	savedNativeProfile                                            sql.NullString
	savedResumeSlot                                               sql.NullInt64
}

func restoreSnapshotCompatible(result launchSnapshot, binding restoreBinding) bool {
	adapterCompatible := result.adapterABI == binding.savedAdapterABI
	if result.purpose == "PRODUCT" && (result.runtimeFamily == "RPGMAKER" || result.runtimeFamily == "ONS" ||
		result.runtimeFamily == "KIRIKIRI") {
		adapterCompatible = binding.savedSaveABI != "" && result.saveABI != ""
	}
	return result.adapterABI != "" && result.saveABI != "" && result.dependencySHA256 != "" &&
		adapterCompatible && result.dependencySHA256 == binding.savedDependency &&
		binding.storedPayloadKind == result.payloadKind && binding.savedSize >= 1 &&
		binding.savedSize <= result.payloadMaxBytes
}

func bindRestoreSnapshot(result launchSnapshot, binding restoreBinding) (launchSnapshot, error) {
	result.gameID = binding.gameID.String
	result.contentRevisionID = binding.contentID.String
	result.variantRevisionID = binding.variantID.String
	result.validationID = binding.validationID.String
	if result.purpose == "PRODUCT" {
		if binding.productGeneration.Valid {
			result.generation = binding.productGeneration.String
			result.dependencySHA256 = binding.productDependency.String
		} else if binding.variantDependencyJSON.Valid {
			digest := sha256.Sum256([]byte(binding.variantDependencyJSON.String))
			result.dependencySHA256 = hex.EncodeToString(digest[:])
		}
	} else {
		result.generation = binding.validationGeneration.String
		result.adapterABI = binding.validationABI.String
		result.dependencySHA256 = binding.validationDependency.String
	}
	if !restoreSnapshotCompatible(result, binding) {
		return launchSnapshot{}, ErrCheckpointIncompatible
	}
	return result, nil
}

func loadLaunchForRestore(
	ctx context.Context, database queryRower, launchID string,
) (launchSnapshot, string, sql.NullString, sql.NullInt64, int64, error) {
	var result launchSnapshot
	var binding restoreBinding
	err := database.QueryRowContext(ctx, `
SELECT launch.purpose,launch.profile_id,launch.game_id,launch.game_content_revision_id,
 launch.game_variant_revision_id,launch.core_artifact_id,launch.rpgmaker_runtime_validation_id,
 artifact.runtime_family,
 artifact.save_payload_kind,artifact.save_max_bytes,
 json_extract(artifact.compatibility_json,'$.adapterAbi'),
 COALESCE(json_extract(artifact.compatibility_json,'$.saveAbi'),
          json_extract(artifact.compatibility_json,'$.adapterAbi')),
 revision.dependency_snapshot_json,
 product_profile.generation,product_profile.adapter_abi,product_profile.dependency_snapshot_sha256,
 validation.generation,validation.adapter_abi,validation.dependency_snapshot_sha256,
 COALESCE(save.payload_sha256,checkpoint.payload_sha256),
 COALESCE(save.payload_size_bytes,checkpoint.size_bytes),
 COALESCE(save.adapter_abi,validation.adapter_abi),
 COALESCE(save.save_abi,
          COALESCE(json_extract(artifact.compatibility_json,'$.saveAbi'),
                   json_extract(artifact.compatibility_json,'$.adapterAbi'))),
 COALESCE(save.dependency_snapshot_sha256,validation.dependency_snapshot_sha256),
 COALESCE(save.payload_kind,checkpoint.payload_kind),
 COALESCE(save.native_profile,checkpoint.native_profile),
 COALESCE(save.resume_slot,checkpoint.resume_slot)
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
LEFT JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
LEFT JOIN rpgmaker_variant_profiles product_profile
  ON product_profile.game_variant_revision_id=launch.game_variant_revision_id
LEFT JOIN rpgmaker_runtime_validations validation
  ON validation.id=launch.rpgmaker_runtime_validation_id
LEFT JOIN save_states save ON save.id=launch.save_state_id AND save.deleted_at_ms IS NULL
LEFT JOIN save_state_runtime_compatibility save_compatibility
  ON save_compatibility.save_state_id=save.id
LEFT JOIN blobs save_blob ON save_blob.id=save.payload_blob_id
LEFT JOIN rpgmaker_runtime_validation_checkpoints checkpoint
  ON checkpoint.validation_id=validation.id AND validation.restore_launch_id=launch.id
LEFT JOIN blobs checkpoint_blob ON checkpoint_blob.id=checkpoint.payload_blob_id
WHERE launch.id=? AND (
 launch.purpose='PRODUCT' AND save.id IS NOT NULL
   AND save.profile_id=launch.profile_id AND save.game_id=launch.game_id
   AND save.game_content_revision_id=launch.game_content_revision_id
   AND save.game_variant_revision_id=launch.game_variant_revision_id
   AND save_compatibility.status='AVAILABLE'
   AND save_blob.sha256=save.payload_sha256 AND save_blob.size_bytes=save.payload_size_bytes
 OR launch.purpose='RPG_RUNTIME_VALIDATION' AND checkpoint.validation_id IS NOT NULL
   AND validation.state IN ('CHECKPOINTED','RESTORED','AWAITING_DECISION')
   AND validation.artifact_id=launch.core_artifact_id AND validation.route_key=launch.route_key
   AND checkpoint_blob.sha256=checkpoint.payload_sha256 AND checkpoint_blob.size_bytes=checkpoint.size_bytes
)
	`, launchID).Scan(
		&result.purpose, &result.profileID, &binding.gameID, &binding.contentID, &binding.variantID,
		&result.artifactID, &binding.validationID,
		&result.runtimeFamily, &result.payloadKind, &result.payloadMaxBytes, &result.adapterABI, &result.saveABI,
		&binding.variantDependencyJSON,
		&binding.productGeneration, &binding.productABI, &binding.productDependency,
		&binding.validationGeneration, &binding.validationABI, &binding.validationDependency,
		&binding.payloadDigest, &binding.savedSize, &binding.savedAdapterABI, &binding.savedSaveABI,
		&binding.savedDependency, &binding.storedPayloadKind, &binding.savedNativeProfile,
		&binding.savedResumeSlot,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return launchSnapshot{}, "", sql.NullString{}, sql.NullInt64{}, 0, ErrCheckpointIncompatible
	}
	if err != nil {
		return launchSnapshot{}, "", sql.NullString{}, sql.NullInt64{}, 0,
			fmt.Errorf("load restore launch: %w", err)
	}
	result, err = bindRestoreSnapshot(result, binding)
	if err != nil {
		return launchSnapshot{}, "", sql.NullString{}, sql.NullInt64{}, 0, ErrCheckpointIncompatible
	}
	return result, binding.payloadDigest, binding.savedNativeProfile, binding.savedResumeSlot, binding.savedSize, nil
}
