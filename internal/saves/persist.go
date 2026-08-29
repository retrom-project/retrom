package saves

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/checkpoint"
)

type nativeBinding struct {
	profile *string
	slot    *int64
}

func (service *Service) CreateManual(
	ctx context.Context,
	launchID, capability, idempotencyKey string,
	request *http.Request,
) (ManualResult, bool, error) {
	launch, err := service.launch(ctx, launchID, capability)
	if err != nil {
		return ManualResult{}, false, err
	}
	if launch.purpose == "RPG_RUNTIME_VALIDATION" && !launch.originalValidationLaunch {
		return ManualResult{}, false, ErrCheckpointUnavailable
	}
	parsed, err := service.parseManual(request, launch)
	if err != nil {
		return ManualResult{}, false, err
	}
	binding, err := validatePayloadBinding(launch, parsed.payload.Path)
	if err != nil {
		return ManualResult{}, false, err
	}
	metadataDigest, _ := json.Marshal(parsed.metadata)
	screenshotDigest := ""
	if parsed.screenshot != nil {
		screenshotDigest = parsed.screenshot.SHA256
	}
	digest := sha256.Sum256([]byte(string(metadataDigest) + "\x00" + parsed.payload.SHA256 + "\x00" + screenshotDigest))
	return service.persistManualSave(
		ctx, launchID, idempotencyKey, hex.EncodeToString(digest[:]), launch, parsed, binding,
	)
}

func validatePayloadBinding(launch launchSnapshot, path string) (nativeBinding, error) {
	if launch.payloadKind == "RUNTIME_STATE" {
		if launch.generation != "" && launch.generation != "RPGXP" && launch.generation != "RPGVX" &&
			launch.generation != "RPGVXACE" {
			return nativeBinding{}, ErrCheckpointInvalid
		}
		return nativeBinding{}, nil
	}
	if launch.payloadKind == "ONS_SAVE_BUNDLE_V1" {
		if launch.generation != "" {
			return nativeBinding{}, ErrCheckpointInvalid
		}
		return nativeBinding{}, nil
	}
	if launch.payloadKind != "NATIVE_SAVE_BUNDLE_V1" {
		return nativeBinding{}, ErrCheckpointInvalid
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nativeBinding{}, fmt.Errorf("saves/service: %w", err)
	}
	bundle, err := checkpoint.Decode(contents)
	if err != nil {
		return nativeBinding{}, ErrCheckpointInvalid
	}
	profile, engine, valid := expectedNativeProfile(launch.generation)
	if !valid || bundle.Manifest.Engine != engine {
		return nativeBinding{}, ErrCheckpointInvalid
	}
	slot := bundle.Manifest.ResumeSlot
	return nativeBinding{profile: &profile, slot: &slot}, nil
}

func expectedNativeProfile(generation string) (string, checkpoint.Engine, bool) {
	switch generation {
	case "RPG2000":
		return "EASYRPG_V1", checkpoint.EngineRPG2000, true
	case "RPG2003":
		return "EASYRPG_V1", checkpoint.EngineRPG2003, true
	case "RPGMV":
		return "RPGMV_V1", checkpoint.EngineRPGMV, true
	case "RPGMZ":
		return "RPGMZ_V1", checkpoint.EngineRPGMZ, true
	default:
		return "", "", false
	}
}

func (service *Service) persistManualSave(
	ctx context.Context,
	launchID, idempotencyKey, requestDigest string,
	launch launchSnapshot,
	parsed parsedManual,
	binding nativeBinding,
) (ManualResult, bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if previous, replayed, replayErr := service.replayManualSave(
		ctx, transaction, launch.principalID, idempotencyKey, requestDigest,
	); replayErr != nil || replayed {
		return previous, replayed, replayErr
	}
	if err := service.ensureWritable(ctx, transaction, launchID, launch); err != nil {
		return ManualResult{}, false, err
	}
	now := service.now().UnixMilli()
	payloadID, err := blobstore.EnsureRecord(ctx, transaction, parsed.payload, "application/octet-stream", now)
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	var result ManualResult
	if launch.purpose == "PRODUCT" {
		result, err = service.insertProductSave(ctx, transaction, launchID, launch, parsed, binding, payloadID, now)
	} else {
		result, err = service.insertValidationCheckpoint(
			ctx, transaction, launch, parsed, binding, payloadID, now,
		)
	}
	if err != nil {
		return ManualResult{}, false, err
	}
	body, _ := json.Marshal(result)
	_, err = transaction.ExecContext(ctx, `
INSERT INTO idempotency_records(principal_id,operation_id,key,request_digest,http_status,
 response_headers_json,response_body,created_at_ms,expires_at_ms)
VALUES(?,'postRuntimeSaveState',?,?,201,'{}',?,?,?)
`, launch.principalID, idempotencyKey, requestDigest, body, now, now+int64(24*time.Hour/time.Millisecond))
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	return result, false, nil
}

func (service *Service) ensureWritable(
	ctx context.Context, transaction *sql.Tx, launchID string, launch launchSnapshot,
) error {
	var writable int
	var query string
	var arguments []any
	if launch.purpose == "PRODUCT" {
		query = `SELECT count(*) FROM launch_sessions launch JOIN games game ON game.id=launch.game_id
WHERE launch.id=? AND launch.game_id=? AND launch.state='ACTIVE' AND game.status='PUBLISHED'`
		arguments = []any{launchID, launch.gameID}
	} else {
		query = `SELECT count(*) FROM launch_sessions launch
JOIN rpgmaker_runtime_validations validation ON validation.id=launch.rpgmaker_runtime_validation_id
WHERE launch.id=? AND launch.id=validation.launch_id AND launch.state='ACTIVE'
 AND validation.id=? AND validation.state='RUNNING'
 AND NOT EXISTS(SELECT 1 FROM rpgmaker_runtime_validation_checkpoints checkpoint
                WHERE checkpoint.validation_id=validation.id)`
		arguments = []any{launchID, launch.validationID}
	}
	if err := transaction.QueryRowContext(ctx, query, arguments...).Scan(&writable); err != nil {
		return fmt.Errorf("saves/service: %w", err)
	}
	if writable != 1 {
		if launch.purpose == "RPG_RUNTIME_VALIDATION" {
			return ErrCheckpointUnavailable
		}
		return ErrCredential
	}
	return nil
}

func (service *Service) insertProductSave(
	ctx context.Context, transaction *sql.Tx, launchID string, launch launchSnapshot,
	parsed parsedManual, binding nativeBinding, payloadID string, now int64,
) (ManualResult, error) {
	var screenshotID any
	var screenshotURL *string
	if parsed.screenshot != nil {
		id, err := blobstore.EnsureRecord(
			ctx, transaction, *parsed.screenshot, parsed.screenshotMediaType, now,
		)
		if err != nil {
			return ManualResult{}, fmt.Errorf("saves/service: %w", err)
		}
		screenshotID = id
		value := "/content/save-states/"
		screenshotURL = &value
	}
	var activeDuration int64
	_ = transaction.QueryRowContext(ctx, `SELECT active_duration_ms FROM play_sessions WHERE launch_session_id=?`,
		launchID).Scan(&activeDuration)
	generated, err := uuid.NewV7()
	if err != nil {
		return ManualResult{}, fmt.Errorf("saves/service: %w", err)
	}
	result := ManualResult{
		ResourceKind: "SAVE_STATE", SaveStateID: generated.String(), PayloadKind: launch.payloadKind,
		NativeProfile: binding.profile, ResumeSlot: binding.slot, ScreenshotURL: screenshotURL,
		CreatedAtMS: now, Name: parsed.metadata.Name, DiscIndex: parsed.metadata.DiscIndex,
		Version: 1, ActiveDurationMS: activeDuration,
	}
	if screenshotURL != nil {
		value := *screenshotURL + result.SaveStateID + "/screenshot"
		result.ScreenshotURL = &value
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO save_states(
 id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,core_artifact_id,
 adapter_abi,save_abi,dependency_snapshot_sha256,dat_version_id,dos_entry_path,
 payload_blob_id,payload_kind,native_profile,resume_slot,payload_sha256,payload_size_bytes,
 screenshot_blob_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,
 source_launch_session_id,disc_index)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,?)
`, result.SaveStateID, launch.profileID, launch.gameID, launch.contentRevisionID,
		launch.variantRevisionID, launch.artifactID, launch.adapterABI, launch.saveABI, launch.dependencySHA256,
		nullableString(launch.datVersionID), nullableString(launch.dosEntry), payloadID, launch.payloadKind,
		nullablePointer(binding.profile), nullableInt64Pointer(binding.slot), parsed.payload.SHA256,
		parsed.payload.Size, screenshotID, result.Name, activeDuration, now, now, launchID, result.DiscIndex)
	if err != nil {
		return ManualResult{}, fmt.Errorf("saves/service: %w", err)
	}
	return result, nil
}

func (service *Service) insertValidationCheckpoint(
	ctx context.Context, transaction *sql.Tx, launch launchSnapshot,
	parsed parsedManual, binding nativeBinding, payloadID string, now int64,
) (ManualResult, error) {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO rpgmaker_runtime_validation_checkpoints(
 validation_id,payload_blob_id,payload_kind,native_profile,resume_slot,
 payload_sha256,size_bytes,created_at_ms)
VALUES(?,?,?,?,?,?,?,?)
`, launch.validationID, payloadID, launch.payloadKind, nullablePointer(binding.profile),
		nullableInt64Pointer(binding.slot), parsed.payload.SHA256, parsed.payload.Size, now)
	if err != nil {
		return ManualResult{}, fmt.Errorf("saves/service: %w", err)
	}
	return ManualResult{
		ResourceKind: "RPG_RUNTIME_VALIDATION_CHECKPOINT", ValidationID: launch.validationID,
		PayloadKind: launch.payloadKind, NativeProfile: binding.profile, ResumeSlot: binding.slot,
		SizeBytes: parsed.payload.Size, PayloadSHA256: parsed.payload.SHA256, CreatedAtMS: now,
	}, nil
}

func (service *Service) replayManualSave(
	ctx context.Context,
	transaction *sql.Tx,
	principalID, idempotencyKey, requestDigest string,
) (ManualResult, bool, error) {
	var storedDigest string
	var storedBody []byte
	err := transaction.QueryRowContext(ctx, `
SELECT request_digest,response_body FROM idempotency_records
WHERE operation_id='postRuntimeSaveState' AND key=? AND principal_id=? AND expires_at_ms>?
`, idempotencyKey, principalID, service.now().UnixMilli()).Scan(&storedDigest, &storedBody)
	if errors.Is(err, sql.ErrNoRows) {
		return ManualResult{}, false, nil
	}
	if err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(storedDigest), []byte(requestDigest)) != 1 {
		return ManualResult{}, false, ErrSequenceReused
	}
	var previous ManualResult
	if err := json.Unmarshal(storedBody, &previous); err != nil {
		return ManualResult{}, false, fmt.Errorf("saves/service: %w", err)
	}
	return previous, true, nil
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullablePointer(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Pointer(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
