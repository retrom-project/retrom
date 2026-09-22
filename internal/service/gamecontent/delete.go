package gamecontent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const deleteReplayRetention = 24 * time.Hour

// DeleteAdminGame validates and persists the administrative game tombstone in
// one repository transaction. The repository port includes every side effect
// that must be atomic with the game state transition.
func (service *Service) DeleteAdminGame(
	ctx context.Context, request DeleteGameRequest,
) (DeleteGameResult, error) {
	if service.repository == nil || service.now == nil {
		return DeleteGameResult{}, ErrInvalid
	}
	if err := validateDeleteGameRequest(request); err != nil {
		return DeleteGameResult{}, err
	}
	now := request.NowMS
	if now <= 0 {
		now = service.now().UnixMilli()
	}
	var result DeleteGameResult
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		return service.deleteInScope(ctx, scope, request, now, &result)
	})
	if err != nil {
		return DeleteGameResult{}, fmt.Errorf("delete admin game: %w", err)
	}
	if result.PayloadReleaseQueued && service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
	return result, nil
}

func (service *Service) deleteInScope(
	ctx context.Context, scope WriteScope, request DeleteGameRequest, now int64, result *DeleteGameResult,
) error {
	if scope.GameDeletionReader == nil || scope.GameDeletionWriter == nil {
		return ErrInvalid
	}
	replayed, err := readDeleteGameReplay(ctx, scope.GameDeletionReader, request, now, result)
	if err != nil {
		return err
	}
	if replayed {
		return nil
	}
	state, err := scope.GameDeletionReader.LoadDeleteGameState(ctx, request.GameID)
	if errors.Is(err, ErrDeleteGameNotFound) {
		return ErrDeleteGameNotFound
	}
	if err != nil {
		return fmt.Errorf("load game deletion state: %w", err)
	}
	if state.Status != "PUBLISHED" {
		return service.rememberExistingDelete(ctx, scope.GameDeletionWriter, request, state, now, result)
	}
	if state.Version != request.ExpectedVersion {
		return ErrDeleteGameVersionConflict
	}
	if request.ConfirmTitle != state.Title {
		return ErrDeleteGameConfirmationMismatch
	}
	impact, err := scope.GameDeletionReader.DeleteGameImpact(ctx, request.GameID)
	if err != nil {
		return fmt.Errorf("read game deletion impact: %w", err)
	}
	if request.ImpactDigest != impact.ImpactDigest {
		return ErrDeleteGameImpactStale
	}
	return service.persistNewDelete(ctx, scope.GameDeletionWriter, request, impact, now, result)
}

func readDeleteGameReplay(
	ctx context.Context,
	reader DeleteGameReader,
	request DeleteGameRequest,
	now int64,
	result *DeleteGameResult,
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

func validateDeleteGameRequest(request DeleteGameRequest) error {
	if request.GameID == "" || request.PrincipalID == "" || request.Key == "" ||
		request.RequestDigest == "" || request.ExpectedVersion < 1 || request.ImpactDigest == "" {
		return ErrInvalid
	}
	return nil
}

func (service *Service) rememberExistingDelete(
	ctx context.Context,
	repository DeleteGameWriter,
	request DeleteGameRequest,
	state DeleteGameState,
	now int64,
	result *DeleteGameResult,
) error {
	response := DeleteGameResponse{
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
	repository DeleteGameWriter,
	request DeleteGameRequest,
	impact DeleteGameImpact,
	now int64,
	result *DeleteGameResult,
) error {
	releaseJob, err := repository.ScheduleGameDeletion(ctx, request.GameID, request.ExpectedVersion, now)
	if err != nil {
		return fmt.Errorf("schedule deleted game payload release: %w", err)
	}
	if err := repository.TransitionDeletedGameRuntime(ctx, request.GameID, now); err != nil {
		return fmt.Errorf("transition deleted game runtime: %w", err)
	}
	if err := repository.RecordDeleteGameAudit(ctx, DeleteGameAudit{
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
	response := DeleteGameResponse{
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

func encodeDeleteGameResponse(response DeleteGameResponse, etag string) ([]byte, string, error) {
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
	repository DeleteGameWriter,
	request DeleteGameRequest,
	status int,
	headers string,
	body []byte,
	now int64,
) error {
	if err := repository.StoreDeleteGameReplay(ctx, DeleteGameReplayWrite{
		PrincipalID: request.PrincipalID, Key: request.Key, RequestDigest: request.RequestDigest,
		HTTPStatus: status, HeadersJSON: headers, Body: body,
		CreatedAtMS: now, ExpiresAtMS: now + deleteReplayRetention.Milliseconds(),
	}); err != nil {
		return fmt.Errorf("store deleted game replay: %w", err)
	}
	return nil
}

func deleteGameAuditImpact(impact DeleteGameImpact) map[string]any {
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
		"sourceKinds":        impact.SourceKinds,
	}
}
