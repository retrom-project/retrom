package gamecontent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	model "retrom/internal/model/gamecontent"
)

const deleteReplayRetention = 24 * time.Hour

// DeleteAdminGame validates and persists the administrative game tombstone in
// one repository transaction. The repository port includes every side effect
// that must be atomic with the game state transition.
func (service *Service) DeleteAdminGame(
	ctx context.Context, request model.DeleteGameRequest,
) (model.DeleteGameResult, error) {
	if service.repository == nil || service.now == nil {
		return model.DeleteGameResult{}, model.ErrInvalid
	}
	if err := validateDeleteGameRequest(request); err != nil {
		return model.DeleteGameResult{}, err
	}
	now := request.NowMS
	if now <= 0 {
		now = service.now().UnixMilli()
	}
	var result model.DeleteGameResult
	err := service.repository.CommitWrite(ctx, func(scope model.WriteScope) error {
		return service.deleteInScope(ctx, scope, request, now, &result)
	})
	if err != nil {
		return model.DeleteGameResult{}, fmt.Errorf("delete admin game: %w", err)
	}
	if result.PayloadReleaseQueued && service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
	return result, nil
}

func (service *Service) deleteInScope(
	ctx context.Context, scope model.WriteScope, request model.DeleteGameRequest, now int64, result *model.DeleteGameResult,
) error {
	if scope.GameDeletionReader == nil || scope.GameDeletionWriter == nil {
		return model.ErrInvalid
	}
	replayed, err := readDeleteGameReplay(ctx, scope.GameDeletionReader, request, now, result)
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
		return service.rememberExistingDelete(ctx, scope.GameDeletionWriter, request, state, now, result)
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
	return service.persistNewDelete(ctx, scope.GameDeletionWriter, request, impact, now, result)
}

func readDeleteGameReplay(
	ctx context.Context,
	reader model.DeleteGameReader,
	request model.DeleteGameRequest,
	now int64,
	result *model.DeleteGameResult,
) (bool, error) {
	replay, found, err := reader.LoadDeleteGameReplay(ctx, request.PrincipalID, request.Key, now)
	if err != nil {
		return false, fmt.Errorf("load game deletion replay: %w", err)
	}
	if found {
		result.Replay = replay
		result.Replayed = true
	}
	return found, nil
}

func validateDeleteGameRequest(request model.DeleteGameRequest) error {
	if request.GameID == "" || request.PrincipalID == "" || request.Key == "" ||
		request.RequestDigest == "" || request.ExpectedVersion < 1 || request.ImpactDigest == "" {
		return model.ErrInvalid
	}
	return nil
}

func (service *Service) rememberExistingDelete(
	ctx context.Context,
	repository model.DeleteGameWriter,
	request model.DeleteGameRequest,
	state model.DeleteGameState,
	now int64,
	result *model.DeleteGameResult,
) error {
	response := model.DeleteGameResponse{
		GameID: request.GameID, Status: state.Status,
		PayloadState: state.PayloadState, PayloadReleaseJobID: state.PayloadReleaseJobID,
	}
	etag := fmt.Sprintf(`"v%d"`, state.Version)
	body, headers, err := encodeDeleteGameResponse(response, etag)
	if err != nil {
		return err
	}
	if err := storeDeleteGameReplay(ctx, repository, request, http.StatusOK, headers, body, now); err != nil {
		return err
	}
	result.HTTPStatus = http.StatusOK
	result.ETag = etag
	result.Body = body
	return nil
}

func (service *Service) persistNewDelete(
	ctx context.Context,
	repository model.DeleteGameWriter,
	request model.DeleteGameRequest,
	impact model.DeleteGameImpact,
	now int64,
	result *model.DeleteGameResult,
) error {
	releaseJob, err := repository.ScheduleGameDeletion(ctx, request.GameID, request.ExpectedVersion, now)
	if err != nil {
		return fmt.Errorf("schedule deleted game payload release: %w", err)
	}
	if err := repository.TransitionDeletedGameRuntime(ctx, request.GameID, now); err != nil {
		return fmt.Errorf("transition deleted game runtime: %w", err)
	}
	if err := repository.RecordDeleteGameAudit(ctx, model.DeleteGameAudit{
		GameID: request.GameID,
		Actor:  request.Actor,
		Before: map[string]any{"status": "PUBLISHED"},
		After: map[string]any{
			"status": "DELETED", "payloadState": "RELEASING", "payloadReleaseJobId": releaseJob,
			"impact": deleteGameAuditImpact(impact),
		},
		NowMS: now,
	}); err != nil {
		return fmt.Errorf("record deleted game audit: %w", err)
	}
	response := model.DeleteGameResponse{
		GameID: request.GameID, Status: "DELETED", PayloadState: "RELEASING",
		PayloadReleaseJobID: &releaseJob,
	}
	etag := fmt.Sprintf(`"v%d"`, request.ExpectedVersion+1)
	body, headers, err := encodeDeleteGameResponse(response, etag)
	if err != nil {
		return err
	}
	if err := storeDeleteGameReplay(ctx, repository, request, http.StatusAccepted, headers, body, now); err != nil {
		return err
	}
	result.HTTPStatus = http.StatusAccepted
	result.ETag = etag
	result.Body = body
	result.PayloadReleaseQueued = true
	return nil
}

func encodeDeleteGameResponse(response model.DeleteGameResponse, etag string) ([]byte, string, error) {
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

func storeDeleteGameReplay(
	ctx context.Context,
	repository model.DeleteGameWriter,
	request model.DeleteGameRequest,
	status int,
	headers string,
	body []byte,
	now int64,
) error {
	if err := repository.StoreDeleteGameReplay(ctx, model.DeleteGameReplayWrite{
		PrincipalID: request.PrincipalID, Key: request.Key, RequestDigest: request.RequestDigest,
		HTTPStatus: status, HeadersJSON: headers, Body: body,
		CreatedAtMS: now, ExpiresAtMS: now + deleteReplayRetention.Milliseconds(),
	}); err != nil {
		return fmt.Errorf("store deleted game replay: %w", err)
	}
	return nil
}

func deleteGameAuditImpact(impact model.DeleteGameImpact) map[string]any {
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
