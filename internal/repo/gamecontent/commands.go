package gamecontent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/content/multidisc"
	coremodel "retrom/internal/model/corevalidation"
	model "retrom/internal/model/gamecontent"
	payloadmodel "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Snapshot reads
// ---------------------------------------------------------------------------

func (repository *Repository) ReadInput(
	ctx context.Context, jobID string, executionNo int64,
) (model.StoredInput, error) {
	tx, err := repository.database.BeginTx(ctx, readOnlyTxOpts())
	if err != nil {
		return model.StoredInput{}, fmt.Errorf("begin content input read: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := readScope(tx)
	stored, err := scope.Inputs.Input(ctx, jobID, executionNo)
	if err != nil {
		return model.StoredInput{}, fmt.Errorf("read replacement input: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.StoredInput{}, fmt.Errorf("commit content input read: %w", err)
	}
	return stored, nil
}

func (repository *Repository) ReadFiles(
	ctx context.Context, uploadSessionID string,
) ([]model.UploadedFile, error) {
	tx, err := repository.database.BeginTx(ctx, readOnlyTxOpts())
	if err != nil {
		return nil, fmt.Errorf("begin content files read: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := readScope(tx)
	files, err := scope.Content.Files(ctx, uploadSessionID)
	if err != nil {
		return nil, fmt.Errorf("read replacement files: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit content files read: %w", err)
	}
	return files, nil
}

func (repository *Repository) ReadAdminGame(
	ctx context.Context, gameID string,
) (model.AdminGameDetail, error) {
	tx, err := repository.database.BeginTx(ctx, readOnlyTxOpts())
	if err != nil {
		return model.AdminGameDetail{}, fmt.Errorf("begin admin game read: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := readScope(tx)
	detail, err := scope.Admin.AdminGame(ctx, gameID)
	if err != nil {
		return model.AdminGameDetail{}, fmt.Errorf("read admin game detail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.AdminGameDetail{}, fmt.Errorf("commit admin game read: %w", err)
	}
	return detail, nil
}

// ---------------------------------------------------------------------------
// Simple lease commands
// ---------------------------------------------------------------------------

func (repository *Repository) CommitClaimLease(
	ctx context.Context, claim model.Claim,
) (bool, error) {
	tx, scope, err := beginWrite(ctx, repository.database)
	if err != nil {
		return false, err
	}
	defer dbexec.Rollback(tx)
	claimed, err := scope.Leases.Claim(ctx, claim)
	if err != nil {
		return false, fmt.Errorf("claim replacement execution: %w", err)
	}
	if err := commitScope(tx); err != nil {
		return false, err
	}
	return claimed, nil
}

func (repository *Repository) CommitRefreshLease(
	ctx context.Context, claim model.Claim, nowMS int64,
) (bool, error) {
	tx, scope, err := beginWrite(ctx, repository.database)
	if err != nil {
		return false, err
	}
	defer dbexec.Rollback(tx)
	current, err := scope.Leases.Refresh(ctx, claim, nowMS)
	if err != nil {
		return false, fmt.Errorf("refresh replacement lease: %w", err)
	}
	if err := commitScope(tx); err != nil {
		return false, err
	}
	return current, nil
}

// ---------------------------------------------------------------------------
// CommitPatchGame
// ---------------------------------------------------------------------------

func (repository *Repository) CommitPatchGame(
	ctx context.Context, request model.AdminGamePatchRequest,
) (model.AdminGamePatchResult, error) {
	tx, scope, err := beginWrite(ctx, repository.database)
	if err != nil {
		return model.AdminGamePatchResult{}, err
	}
	defer dbexec.Rollback(tx)
	state, err := scope.AdminWriter.LoadPatchState(ctx, request.GameID)
	if errors.Is(err, model.ErrAdminGameNotFound) {
		return model.AdminGamePatchResult{}, model.ErrAdminGameNotFound
	}
	if err != nil {
		return model.AdminGamePatchResult{}, fmt.Errorf("load admin game patch state: %w", err)
	}
	if state.Version != request.ExpectedVersion || state.Status != "PUBLISHED" {
		return model.AdminGamePatchResult{}, model.ErrAdminGameVersionConflict
	}
	applyAdminGamePatch(&state.Metadata, request)
	ok, err := scope.AdminWriter.UpdatePatch(ctx, model.AdminGamePatchUpdate{
		GameID: request.GameID, ExpectedVersion: request.ExpectedVersion,
		Metadata: state.Metadata, Actor: request.Actor, NowMS: request.NowMS,
	})
	if err != nil {
		return model.AdminGamePatchResult{}, fmt.Errorf("persist admin game patch: %w", err)
	}
	if !ok {
		return model.AdminGamePatchResult{}, model.ErrAdminGameVersionConflict
	}
	result := model.AdminGamePatchResult{
		Version:     request.ExpectedVersion + 1,
		UpdatedAtMS: request.NowMS,
	}
	if err := commitScope(tx); err != nil {
		return model.AdminGamePatchResult{}, err
	}
	return result, nil
}

func applyAdminGamePatch(metadata *model.AdminGameMetadata, r model.AdminGamePatchRequest) {
	if r.Title != nil {
		metadata.Title = *r.Title
	}
	if r.Description != nil {
		metadata.Description = *r.Description
	}
	if r.Developer != nil {
		metadata.Developer = *r.Developer
	}
	if r.Publisher != nil {
		metadata.Publisher = *r.Publisher
	}
	if r.Genre != nil {
		metadata.Genre = *r.Genre
	}
	if r.PlayersPresent {
		metadata.Players = r.Players
	}
	if r.ReleaseYearPresent {
		metadata.ReleaseYear = r.ReleaseYear
	}
}

// ---------------------------------------------------------------------------
// CommitDeleteGame
// ---------------------------------------------------------------------------

func (repository *Repository) CommitDeleteGame(
	ctx context.Context, request model.DeleteGameRequest,
) (model.DeleteGameResult, error) {
	tx, scope, err := beginWrite(ctx, repository.database)
	if err != nil {
		return model.DeleteGameResult{}, err
	}
	defer dbexec.Rollback(tx)
	var result model.DeleteGameResult
	if err := deleteInScope(ctx, scope, request, &result); err != nil {
		return model.DeleteGameResult{}, err
	}
	if err := commitScope(tx); err != nil {
		return model.DeleteGameResult{}, err
	}
	return result, nil
}

func deleteInScope(
	ctx context.Context, scope model.WriteScope,
	request model.DeleteGameRequest, result *model.DeleteGameResult,
) error {
	replayed, err := readDeleteReplay(ctx, scope.GameDeletionReader, request, result)
	if err != nil {
		return err
	}
	if replayed {
		return nil
	}
	state, err := scope.GameDeletionReader.LoadDeleteGameState(ctx, request.GameID)
	if errors.Is(err, model.ErrDeleteGameNotFound) {
		return model.ErrDeleteGameNotFound
	}
	if err != nil {
		return fmt.Errorf("load game deletion state: %w", err)
	}
	if state.Status != "PUBLISHED" {
		return rememberExistingDelete(ctx, scope.GameDeletionWriter, request, state, result)
	}
	if state.Version != request.ExpectedVersion {
		return model.ErrDeleteGameVersionConflict
	}
	if request.ConfirmTitle != state.Title {
		return model.ErrDeleteGameConfirmationMismatch
	}
	impact, err := scope.GameDeletionReader.DeleteGameImpact(ctx, request.GameID)
	if err != nil {
		return fmt.Errorf("read game deletion impact: %w", err)
	}
	if request.ImpactDigest != impact.ImpactDigest {
		return model.ErrDeleteGameImpactStale
	}
	return persistNewDelete(ctx, scope.GameDeletionWriter, request, impact, result)
}

func readDeleteReplay(
	ctx context.Context, reader model.DeleteGameReader,
	request model.DeleteGameRequest, result *model.DeleteGameResult,
) (bool, error) {
	replay, found, err := reader.LoadDeleteGameReplay(
		ctx, request.PrincipalID, request.Key, request.NowMS,
	)
	if err != nil {
		return false, fmt.Errorf("load game deletion replay: %w", err)
	}
	if found {
		result.Replay = replay
		result.Replayed = true
	}
	return found, nil
}

func rememberExistingDelete(
	ctx context.Context, writer model.DeleteGameWriter,
	request model.DeleteGameRequest, state model.DeleteGameState,
	result *model.DeleteGameResult,
) error {
	response := model.DeleteGameResponse{
		GameID: request.GameID, Status: state.Status,
		PayloadState:        state.PayloadState,
		PayloadReleaseJobID: state.PayloadReleaseJobID,
	}
	etag := fmt.Sprintf(`"v%d"`, state.Version)
	body, headers, err := encodeDeleteResponse(response, etag)
	if err != nil {
		return err
	}
	if err := storeDeleteReplay(
		ctx, writer, request, http.StatusOK, headers, body,
	); err != nil {
		return err
	}
	result.HTTPStatus = http.StatusOK
	result.ETag = etag
	result.Body = body
	return nil
}

func persistNewDelete(
	ctx context.Context, writer model.DeleteGameWriter,
	request model.DeleteGameRequest, impact model.DeleteGameImpact,
	result *model.DeleteGameResult,
) error {
	releaseJob, err := writer.ScheduleGameDeletion(
		ctx, request.GameID, request.ExpectedVersion, request.NowMS,
	)
	if err != nil {
		return fmt.Errorf("schedule deleted game payload release: %w", err)
	}
	if err := writer.TransitionDeletedGameRuntime(ctx, request.GameID, request.NowMS); err != nil {
		return fmt.Errorf("transition deleted game runtime: %w", err)
	}
	if err := writer.RecordDeleteGameAudit(ctx, model.DeleteGameAudit{
		GameID: request.GameID,
		Actor:  request.Actor,
		Before: map[string]any{"status": "PUBLISHED"},
		After: map[string]any{
			"status": "DELETED", "payloadState": "RELEASING",
			"payloadReleaseJobId": releaseJob,
			"impact":              deleteAuditImpact(impact),
		},
		NowMS: request.NowMS,
	}); err != nil {
		return fmt.Errorf("record deleted game audit: %w", err)
	}
	response := model.DeleteGameResponse{
		GameID: request.GameID, Status: "DELETED", PayloadState: "RELEASING",
		PayloadReleaseJobID: &releaseJob,
	}
	etag := fmt.Sprintf(`"v%d"`, request.ExpectedVersion+1)
	body, headers, err := encodeDeleteResponse(response, etag)
	if err != nil {
		return err
	}
	if err := storeDeleteReplay(
		ctx, writer, request, http.StatusAccepted, headers, body,
	); err != nil {
		return err
	}
	result.HTTPStatus = http.StatusAccepted
	result.ETag = etag
	result.Body = body
	result.PayloadReleaseQueued = true
	return nil
}

func encodeDeleteResponse(
	response model.DeleteGameResponse, etag string,
) ([]byte, string, error) {
	body, err := json.Marshal(response)
	if err != nil {
		return nil, "", fmt.Errorf("encode deleted game response: %w", err)
	}
	body = append(body, '\n')
	headers, err := json.Marshal(map[string]string{
		"Cache-Control": "private, no-store",
		"Content-Type":  "application/json; charset=utf-8",
		"ETag":          etag,
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode deleted game response headers: %w", err)
	}
	return body, string(headers), nil
}

const deleteReplayRetention = 24 * time.Hour

func storeDeleteReplay(
	ctx context.Context, writer model.DeleteGameWriter,
	request model.DeleteGameRequest, status int, headers string, body []byte,
) error {
	if err := writer.StoreDeleteGameReplay(ctx, model.DeleteGameReplayWrite{
		PrincipalID: request.PrincipalID, Key: request.Key,
		RequestDigest: request.RequestDigest,
		HTTPStatus:    status, HeadersJSON: headers, Body: body,
		CreatedAtMS: request.NowMS,
		ExpiresAtMS: request.NowMS + deleteReplayRetention.Milliseconds(),
	}); err != nil {
		return fmt.Errorf("store deleted game replay: %w", err)
	}
	return nil
}

func deleteAuditImpact(impact model.DeleteGameImpact) map[string]any {
	return map[string]any{
		"registeredBytes":    impact.RegisteredBytes,
		"exclusiveBytes":     impact.ExclusiveBytes,
		"sharedBytes":        impact.SharedBytes,
		"blobCount":          impact.BlobCount,
		"saveStateCount":     impact.SaveStateCount,
		"assetCount":         impact.AssetCount,
		"contentFileCount":   impact.ContentFileCount,
		"activeLaunchCount":  impact.ActiveLaunchCount,
		"activeNetplayCount": impact.ActiveNetplayCount,
		"reviewEventCount":   impact.ReviewEventCount,
		"sourceKinds":        impact.SourceKinds,
	}
}

// ---------------------------------------------------------------------------
// CommitSchedule
// ---------------------------------------------------------------------------

func (repository *Repository) CommitSchedule(
	ctx context.Context, cmd model.ScheduleCommand,
) (model.ScheduleResult, error) {
	tx, scope, err := beginWrite(ctx, repository.database)
	if err != nil {
		return model.ScheduleResult{}, err
	}
	defer dbexec.Rollback(tx)
	result, err := scheduleInScope(ctx, scope, cmd)
	if err != nil {
		return model.ScheduleResult{}, fmt.Errorf("schedule content replacement: %w", err)
	}
	if err := commitScope(tx); err != nil {
		return model.ScheduleResult{}, err
	}
	return result, nil
}

func scheduleInScope(
	ctx context.Context, scope model.WriteScope, cmd model.ScheduleCommand,
) (model.ScheduleResult, error) {
	stored, found, err := loadScheduleReplay(ctx, scope.Replays, cmd)
	if err != nil {
		return model.ScheduleResult{}, err
	}
	if found {
		return stored, nil
	}
	result, err := scheduleFresh(ctx, scope, cmd)
	if err != nil {
		return model.ScheduleResult{}, err
	}
	if cmd.Key != "" {
		if err := rememberSchedule(ctx, scope.Replays, result, cmd); err != nil {
			return model.ScheduleResult{}, err
		}
	}
	return result, nil
}

func loadScheduleReplay(
	ctx context.Context, replays model.ReplayRecords, cmd model.ScheduleCommand,
) (model.ScheduleResult, bool, error) {
	if cmd.Key == "" {
		return model.ScheduleResult{}, false, nil
	}
	stored, found, err := replays.Load(ctx, cmd.PrincipalID, cmd.Key, cmd.NowMS)
	if err != nil {
		return model.ScheduleResult{}, false, fmt.Errorf("read replacement replay: %w", err)
	}
	if !found {
		return model.ScheduleResult{}, false, nil
	}
	if stored.Digest != cmd.Digest {
		return model.ScheduleResult{}, false, model.ErrIdempotencyKeyReused
	}
	var result model.ScheduleResult
	if err := json.Unmarshal(stored.Body, &result); err != nil {
		return model.ScheduleResult{}, false, fmt.Errorf(
			"%w: decode replay: %w", model.ErrInvalid, err,
		)
	}
	result.Replayed = true
	return result, true, nil
}

func scheduleFresh(
	ctx context.Context, scope model.WriteScope, cmd model.ScheduleCommand,
) (model.ScheduleResult, error) {
	binding, err := scope.Content.Binding(ctx, cmd.GameID)
	if err != nil {
		return model.ScheduleResult{}, fmt.Errorf("load replacement binding: %w", err)
	}
	if binding.Version != cmd.ExpectedVersion {
		return model.ScheduleResult{}, model.ErrInvalid
	}
	if err := validateScheduleMode(binding.PlatformID, cmd.ContentMode); err != nil {
		return model.ScheduleResult{}, err
	}
	caps := contentcapability.Resolve(
		binding.PlatformID, true, cmd.MultiDiscImportEnabled, binding.ContentPolicy,
	)
	if cmd.ContentMode == contentcapability.ModeMultiDisc && caps.MultiDisc == nil {
		return model.ScheduleResult{}, model.ErrInvalid
	}
	upload, err := scope.Content.Upload(ctx, cmd.UploadID)
	if err != nil {
		return model.ScheduleResult{}, fmt.Errorf("load replacement upload: %w", err)
	}
	if err := validateUpload(upload, cmd.ContentMode, binding.PlatformID); err != nil {
		return model.ScheduleResult{}, err
	}
	snapshot, err := buildScheduleSnapshot(binding, caps, cmd)
	if err != nil {
		return model.ScheduleResult{}, err
	}
	return enqueueJob(ctx, scope.Jobs, snapshot, cmd.NowMS)
}

func validateScheduleMode(platformID, contentMode string) error {
	if platformID == "rpgmaker" && contentMode != contentcapability.ModeRPGMakerProject ||
		platformID != "rpgmaker" && contentMode == contentcapability.ModeRPGMakerProject {
		return model.ErrInvalid
	}
	return nil
}

func validateUpload(upload model.Upload, mode, platformID string) error {
	if upload.State != "COMPLETE" || upload.FileCount == 0 || upload.Consumptions != 0 {
		return model.ErrInvalid
	}
	if mode == contentcapability.ModeStandard && platformID != "dos" && upload.FileCount != 1 {
		return model.ErrInvalid
	}
	if (mode == contentcapability.ModeMultiDisc || mode == contentcapability.ModeRPGMakerProject) &&
		upload.SourceType != "DIRECTORY" {
		return model.ErrInvalid
	}
	return nil
}

func buildScheduleSnapshot(
	binding model.Binding, caps contentcapability.ImportCapabilities, cmd model.ScheduleCommand,
) (model.JobSnapshot, error) {
	execution, err := uuid.NewV7()
	if err != nil {
		return model.JobSnapshot{}, fmt.Errorf("create replacement execution identity: %w", err)
	}
	policyDigest := binding.ContentPolicy.Digest()
	configInput := fmt.Sprintf(
		"%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s",
		binding.InstanceID, binding.PlatformVersion, binding.ProviderID,
		binding.TargetID, policyDigest, cmd.ContentMode, pointerText(binding.DATID),
	)
	configDigest := sha256.Sum256([]byte(configInput))
	snapshot := model.JobSnapshot{
		ExecutionID:             execution.String(),
		GameID:                  cmd.GameID,
		GameVersion:             cmd.ExpectedVersion,
		BaseManifestDigest:      binding.ManifestDigest,
		UploadSessionID:         cmd.UploadID,
		PlatformID:              binding.PlatformID,
		PlatformInstanceID:      binding.InstanceID,
		PlatformInstanceVersion: binding.PlatformVersion,
		CoreID:                  binding.CoreID,
		ProviderID:              binding.ProviderID,
		TargetID:                binding.TargetID,
		ContentPolicy:           binding.ContentPolicy,
		TargetPolicyDigest:      policyDigest,
		ContentMode:             cmd.ContentMode,
		DATVersionID:            binding.DATID,
		ConfigSnapshotDigest:    hex.EncodeToString(configDigest[:]),
		VariantID:               binding.VariantID,
		RPGGeneration:           binding.RPGGeneration,
		RPGDependencySHA256:     binding.RPGDependencySHA256,
		RPGRequirementsSHA256:   binding.RPGRequirementsSHA256,
	}
	if caps.MultiDisc != nil && cmd.ContentMode == contentcapability.ModeMultiDisc {
		snapshot.MaxDiscs = caps.MultiDisc.MaxDiscs
		snapshot.MaxTotalBytes = caps.MultiDisc.MaxTotalBytes
	}
	return snapshot, nil
}

type scheduleEnvelope struct {
	SchemaVersion int               `json:"schemaVersion"`
	Kind          string            `json:"kind"`
	Scope         scheduleScope     `json:"scope"`
	ExecutionID   string            `json:"executionId"`
	Inputs        model.JobSnapshot `json:"inputs"`
}
type scheduleScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func enqueueJob(
	ctx context.Context, jobs model.JobWriter, snapshot model.JobSnapshot, nowMS int64,
) (model.ScheduleResult, error) {
	jobID, err := uuid.NewV7()
	if err != nil {
		return model.ScheduleResult{}, fmt.Errorf("create replacement job identity: %w", err)
	}
	consumptionID, err := uuid.NewV7()
	if err != nil {
		return model.ScheduleResult{}, fmt.Errorf("create replacement consumption identity: %w", err)
	}
	envelope := scheduleEnvelope{
		SchemaVersion: 1, Kind: "GAME_CONTENT_REPLACE",
		Scope:       scheduleScope{"GAME", snapshot.GameID},
		ExecutionID: snapshot.ExecutionID, Inputs: snapshot,
	}
	input, err := json.Marshal(envelope)
	if err != nil {
		return model.ScheduleResult{}, fmt.Errorf("encode replacement input: %w", err)
	}
	dedupeInput, err := json.Marshal(map[string]any{
		"executionId": snapshot.ExecutionID, "gameId": snapshot.GameID,
	})
	if err != nil {
		return model.ScheduleResult{}, fmt.Errorf("encode replacement identity: %w", err)
	}
	dedupe := sha256.Sum256(append(
		[]byte("retrom-job-dedupe-v1\x00GAME_CONTENT_REPLACE\x00"), dedupeInput...,
	))
	inputDigest := sha256.Sum256(input)
	if err := jobs.Enqueue(ctx, model.ScheduleWrite{
		JobID: jobID.String(), ConsumptionID: consumptionID.String(),
		GameID: snapshot.GameID, UploadID: snapshot.UploadSessionID,
		Dedupe:      hex.EncodeToString(dedupe[:]),
		InputDigest: hex.EncodeToString(inputDigest[:]),
		Input:       input, Now: nowMS,
	}); err != nil {
		return model.ScheduleResult{}, fmt.Errorf("persist replacement job: %w", err)
	}
	return model.ScheduleResult{
		GameID: snapshot.GameID, JobID: jobID.String(),
		State: "QUEUED", Version: snapshot.GameVersion,
	}, nil
}

func rememberSchedule(
	ctx context.Context, replays model.ReplayRecords,
	result model.ScheduleResult, cmd model.ScheduleCommand,
) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode scheduled replacement: %w", err)
	}
	headers, err := json.Marshal(map[string]string{
		"Content-Type": "application/json; charset=utf-8",
		"ETag":         fmt.Sprintf(`"v%d"`, result.Version),
	})
	if err != nil {
		return fmt.Errorf("encode replacement headers: %w", err)
	}
	if err := replays.Remember(ctx, model.ReplayWrite{
		PrincipalID: cmd.PrincipalID, Key: cmd.Key, Digest: cmd.Digest,
		Headers: headers, Body: body,
		Now:       cmd.NowMS,
		ExpiresAt: cmd.NowMS + int64(24*time.Hour/time.Millisecond),
	}); err != nil {
		return fmt.Errorf("remember scheduled replacement: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// CommitSettleFailure
// ---------------------------------------------------------------------------

func (repository *Repository) CommitSettleFailure(
	ctx context.Context, cmd model.SettleFailureCommand,
) (model.SettleFailureResult, error) {
	tx, scope, err := beginWrite(ctx, repository.database)
	if err != nil {
		return model.SettleFailureResult{}, err
	}
	defer dbexec.Rollback(tx)
	result, err := settleInScope(ctx, scope, cmd)
	if err != nil {
		return model.SettleFailureResult{}, err
	}
	if err := commitScope(tx); err != nil {
		return model.SettleFailureResult{}, err
	}
	return result, nil
}

func settleInScope(
	ctx context.Context, scope model.WriteScope, cmd model.SettleFailureCommand,
) (model.SettleFailureResult, error) {
	state, err := scope.Leases.State(ctx, cmd.Claim)
	if err != nil {
		return model.SettleFailureResult{}, fmt.Errorf(
			"read failed replacement ownership: %w", err,
		)
	}
	if state != "RUNNING" && state != "CANCEL_REQUESTED" {
		return model.SettleFailureResult{}, nil
	}
	outcome := cmd.Outcome
	if state == "CANCEL_REQUESTED" {
		outcome.Cancelled = true
		outcome.Retryable = false
	}
	ok, err := scope.Jobs.Fail(ctx, outcome)
	if err != nil {
		return model.SettleFailureResult{}, fmt.Errorf("finish failed replacement: %w", err)
	}
	if ok && !outcome.Retryable {
		if err := releaseUpload(ctx, scope.Retirements, cmd.Claim.JobID, outcome.Now); err != nil {
			return model.SettleFailureResult{}, fmt.Errorf(
				"release terminal replacement upload: %w", err,
			)
		}
	}
	return model.SettleFailureResult{
		Changed: ok, Retryable: outcome.Retryable,
		SignalRelease: ok && !outcome.Retryable,
	}, nil
}

// ---------------------------------------------------------------------------
// CommitPublish
// ---------------------------------------------------------------------------

func (repository *Repository) CommitPublish(
	ctx context.Context, cmd model.PublishCommand,
) (model.PublishResult, error) {
	tx, scope, err := beginWrite(ctx, repository.database)
	if err != nil {
		return model.PublishResult{}, err
	}
	defer dbexec.Rollback(tx)
	result, err := publishInScope(ctx, scope, repository.gc, cmd)
	if err != nil {
		return model.PublishResult{}, fmt.Errorf("commit replacement publication: %w", err)
	}
	if err := commitScope(tx); err != nil {
		return model.PublishResult{}, err
	}
	return result, nil
}

func publishInScope(
	ctx context.Context, scope model.WriteScope,
	gc payloadmodel.GCStager, cmd model.PublishCommand,
) (model.PublishResult, error) {
	binding, err := verifyPublishBinding(ctx, scope, cmd)
	if err != nil {
		return model.PublishResult{}, err
	}
	dependency, err := resolvePublishDependencies(ctx, scope, cmd, binding)
	if err != nil {
		return model.PublishResult{}, err
	}
	impact, err := retireContent(
		ctx, scope.Retirements, cmd.Snapshot.GameID, cmd.Snapshot.VariantID, cmd.NowMS,
	)
	if err != nil {
		return model.PublishResult{}, fmt.Errorf("retire current game content: %w", err)
	}
	if err := scope.ContentWriter.Publish(ctx, model.Publication{
		JobID: cmd.Claim.JobID, Snapshot: cmd.Snapshot,
		Prepared: cmd.Prepared, DependencySnapshotJSON: dependency,
		Now: cmd.NowMS,
	}); err != nil {
		return model.PublishResult{}, fmt.Errorf("publish replacement content: %w", err)
	}
	if gc != nil {
		if err := gc.StageInScope(ctx, scope.Retirements.GC, impact.CandidateBlobIDs); err != nil {
			return model.PublishResult{}, fmt.Errorf("stage replaced blob candidates: %w", err)
		}
	}
	if err := scope.Jobs.Succeed(ctx, model.Outcome{
		Claim: cmd.Claim, GameID: cmd.Snapshot.GameID,
		ManifestDigest:   cmd.Prepared.ManifestDigest,
		VariantID:        cmd.Snapshot.VariantID,
		RetiredSaveCount: impact.SaveStateCount, Now: cmd.NowMS,
	}); err != nil {
		return model.PublishResult{}, fmt.Errorf("complete replacement execution: %w", err)
	}
	if err := releaseUpload(ctx, scope.Retirements, cmd.Claim.JobID, cmd.NowMS); err != nil {
		return model.PublishResult{}, fmt.Errorf("release completed replacement upload: %w", err)
	}
	return model.PublishResult{SignalRelease: true}, nil
}

func verifyPublishBinding(
	ctx context.Context, scope model.WriteScope, cmd model.PublishCommand,
) (model.Binding, error) {
	current, err := scope.Leases.Current(ctx, cmd.Claim, cmd.NowMS)
	if err != nil {
		return model.Binding{}, fmt.Errorf("check replacement ownership: %w", err)
	}
	if !current {
		return model.Binding{}, model.ErrExecutionLost
	}
	binding, err := scope.Content.Binding(ctx, cmd.Snapshot.GameID)
	if err != nil {
		return model.Binding{}, fmt.Errorf("check current replacement binding: %w", err)
	}
	if !bindingMatchesSnapshot(binding, cmd.Snapshot) ||
		pointerText(binding.DATID) != pointerText(cmd.Snapshot.DATVersionID) {
		return model.Binding{}, &contentValidationError{Code: "GAME_CONTENT_CHANGED"}
	}
	identity, err := scope.Content.Identity(ctx, cmd.Snapshot.GameID)
	if err != nil {
		return model.Binding{}, fmt.Errorf("read current replacement identity: %w", err)
	}
	if cmd.Prepared.ContentKind == multidisc.ContentKind {
		identity = slices.DeleteFunc(identity, func(file model.IdentityFile) bool {
			return file.Role != "DISC"
		})
	}
	if slices.Equal(identity, preparedIdentity(cmd.Prepared)) {
		return model.Binding{}, &contentValidationError{Code: "GAME_CONTENT_UNCHANGED"}
	}
	return binding, nil
}

func resolvePublishDependencies(
	ctx context.Context, scope model.WriteScope,
	cmd model.PublishCommand, binding model.Binding,
) ([]byte, error) {
	if cmd.Prepared.RPGMaker != nil {
		return []byte(binding.DependencySnapshotJSON), nil
	}
	records, err := scope.BIOS.BIOS(ctx, cmd.Snapshot.ProviderID, cmd.Snapshot.TargetID)
	if err != nil {
		return nil, fmt.Errorf("resolve replacement dependencies: %w", err)
	}
	bios, status, code, err := coremodel.ResolveBIOSRecords(
		records, cmd.Prepared.FirstContentLogicalName,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve replacement dependencies: %w", err)
	}
	if status != "READY" {
		return nil, &contentValidationError{Code: code}
	}
	if cmd.Prepared.ContentKind == multidisc.ContentKind {
		bios.MultiDisc = &corevalidation.MultiDiscSnapshot{
			ContentKind:             corevalidation.MultiDiscContentKind,
			ParserVersion:           corevalidation.MultiDiscParserVersion,
			DiscCount:               len(cmd.Prepared.OrderedDiscSHA256),
			MissingEntries:          []corevalidation.MultiDiscMissingEntry{},
			OrderedDiscSHA256:       cmd.Prepared.OrderedDiscSHA256,
			CanonicalPlaylistSHA256: cmd.Prepared.CanonicalPlaylist.SHA256,
			Delivery:                corevalidation.MultiDiscDelivery,
		}
	}
	result, err := bios.JSON()
	if err != nil {
		return nil, fmt.Errorf("encode replacement dependencies: %w", err)
	}
	return result, nil
}

func bindingMatchesSnapshot(binding model.Binding, snapshot model.JobSnapshot) bool {
	return binding.InstanceID == snapshot.PlatformInstanceID &&
		binding.PlatformVersion == snapshot.PlatformInstanceVersion &&
		binding.CoreID == snapshot.CoreID &&
		binding.ProviderID == snapshot.ProviderID &&
		binding.TargetID == snapshot.TargetID &&
		binding.Version == snapshot.GameVersion &&
		binding.VariantID == snapshot.VariantID
}

func preparedIdentity(prepared model.PreparedReplacement) []model.IdentityFile {
	if prepared.ContentKind == multidisc.ContentKind {
		identity := make([]model.IdentityFile, 0, len(prepared.OrderedDiscSHA256))
		for _, digest := range prepared.OrderedDiscSHA256 {
			identity = append(identity, model.IdentityFile{Role: "DISC", SHA256: digest})
		}
		return identity
	}
	result := make([]model.IdentityFile, len(prepared.Files))
	for i, file := range prepared.Files {
		result[i] = model.IdentityFile{Role: file.Role, SHA256: file.SHA256}
	}
	return result
}

type contentValidationError = model.ContentValidationError

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func readOnlyTxOpts() *sql.TxOptions {
	return nil
}
