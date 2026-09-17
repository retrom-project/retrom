package gamecontent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/gamecontent"
)

type deleteGameTestRepository struct {
	model.Repository
	result    model.DeleteGameResult
	resultErr error
	requests  []model.DeleteGameRequest
}

func (repository *deleteGameTestRepository) CommitDeleteGame(
	_ context.Context, request model.DeleteGameRequest,
) (model.DeleteGameResult, error) {
	repository.requests = append(repository.requests, request)
	return repository.result, repository.resultErr
}

type deleteGameTestSignal struct{ calls int }

func (signal *deleteGameTestSignal) Signal() { signal.calls++ }

func validDeleteGameRequest() model.DeleteGameRequest {
	return model.DeleteGameRequest{
		GameID: "game", PrincipalID: "principal", Key: "key", RequestDigest: "digest",
		ConfirmTitle: "Fixture", ImpactDigest: "impact", ExpectedVersion: 1, NowMS: 100,
	}
}

func TestDeleteAdminGamePersistsAtomicWorkflowAndSignalsRelease(t *testing.T) {
	repository := &deleteGameTestRepository{
		result: model.DeleteGameResult{
			HTTPStatus: 202, ETag: `"v2"`, PayloadReleaseQueued: true,
			Body: []byte(`{"gameId":"game","status":"DELETED","payloadState":"RELEASING","payloadReleaseJobId":"release-job"}` + "\n"),
		},
	}
	signal := &deleteGameTestSignal{}
	service := New(repository, func() time.Time { return time.UnixMilli(100) }).WithPayloadRelease(signal)

	result, err := service.DeleteAdminGame(t.Context(), validDeleteGameRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.HTTPStatus != 202 || result.ETag != `"v2"` || result.Replayed || !result.PayloadReleaseQueued {
		t.Fatalf("unexpected delete result: %+v", result)
	}
	if !strings.Contains(string(result.Body), `"payloadReleaseJobId":"release-job"`) {
		t.Fatalf("delete response = %s", result.Body)
	}
	if signal.calls != 1 {
		t.Fatalf("signal calls=%d", signal.calls)
	}
}

func TestDeleteAdminGameRejectsStaleConfirmationBeforeSideEffects(t *testing.T) {
	repository := &deleteGameTestRepository{
		resultErr: model.ErrDeleteGameConfirmationMismatch,
	}
	service := New(repository, time.Now)
	request := validDeleteGameRequest()
	request.ConfirmTitle = "Other"

	_, err := service.DeleteAdminGame(t.Context(), request)
	if !errors.Is(err, model.ErrDeleteGameConfirmationMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestDeleteAdminGameReplaysDurableResponseBeforeReadingGame(t *testing.T) {
	repository := &deleteGameTestRepository{
		result: model.DeleteGameResult{
			Replayed: true,
			Replay: model.DeleteGameReplay{
				RequestDigest: "digest", HTTPStatus: 202, HeadersJSON: `{"ETag":"v2"}`, Body: []byte(`{}` + "\n"),
			},
		},
	}
	service := New(repository, time.Now)

	result, err := service.DeleteAdminGame(t.Context(), validDeleteGameRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replayed || result.Replay.HTTPStatus != 202 {
		t.Fatalf("unexpected replay result: %+v", result)
	}
}
