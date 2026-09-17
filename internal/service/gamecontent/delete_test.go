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
	state         model.DeleteGameState
	impact        model.DeleteGameImpact
	replay        model.DeleteGameReplay
	findReplay    bool
	stateReads    int
	impactReads   int
	scheduleCalls int
	runtimeCalls  int
	auditCalls    int
	replayWrites  []model.DeleteGameReplayWrite
}

func (repository *deleteGameTestRepository) WithRead(
	_ context.Context, _ func(model.ReadScope) error,
) error {
	return nil
}

func (repository *deleteGameTestRepository) CommitWrite(
	_ context.Context, work func(model.WriteScope) error,
) error {
	return work(model.WriteScope{GameDeletionReader: repository, GameDeletionWriter: repository})
}

func (repository *deleteGameTestRepository) LoadDeleteGameState(
	_ context.Context, _ string,
) (model.DeleteGameState, error) {
	repository.stateReads++
	return repository.state, nil
}

func (repository *deleteGameTestRepository) LoadDeleteGameReplay(
	_ context.Context, _, _ string, _ int64,
) (model.DeleteGameReplay, bool, error) {
	return repository.replay, repository.findReplay, nil
}

func (repository *deleteGameTestRepository) DeleteGameImpact(
	_ context.Context, _ string,
) (model.DeleteGameImpact, error) {
	repository.impactReads++
	return repository.impact, nil
}

func (repository *deleteGameTestRepository) ScheduleGameDeletion(
	_ context.Context, _ string, _, _ int64,
) (string, error) {
	repository.scheduleCalls++
	return "release-job", nil
}

func (repository *deleteGameTestRepository) TransitionDeletedGameRuntime(
	_ context.Context, _ string, _ int64,
) error {
	repository.runtimeCalls++
	return nil
}

func (repository *deleteGameTestRepository) RecordDeleteGameAudit(
	_ context.Context, _ model.DeleteGameAudit,
) error {
	repository.auditCalls++
	return nil
}

func (repository *deleteGameTestRepository) StoreDeleteGameReplay(
	_ context.Context, replay model.DeleteGameReplayWrite,
) error {
	repository.replayWrites = append(repository.replayWrites, replay)
	return nil
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
		state:  model.DeleteGameState{Title: "Fixture", Status: "PUBLISHED", Version: 1},
		impact: model.DeleteGameImpact{ImpactDigest: "impact", RegisteredBytes: "42", SourceKinds: []string{"USER_UPLOAD"}},
	}
	signal := &deleteGameTestSignal{}
	service := New(repository, func() time.Time { return time.UnixMilli(100) }).WithPayloadRelease(signal)

	result, err := service.DeleteAdminGame(t.Context(), validDeleteGameRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertFreshDeleteResult(t, result)
	assertFreshDeleteCalls(t, repository, signal)
	assertFreshDeleteReplay(t, repository.replayWrites[0])
}

func assertFreshDeleteResult(t *testing.T, result model.DeleteGameResult) {
	t.Helper()
	if result.HTTPStatus != 202 || result.ETag != `"v2"` || result.Replayed || !result.PayloadReleaseQueued {
		t.Fatalf("unexpected delete result: %+v", result)
	}
	if !strings.Contains(string(result.Body), `"payloadReleaseJobId":"release-job"`) {
		t.Fatalf("delete response = %s", result.Body)
	}
}

func assertFreshDeleteCalls(t *testing.T, repository *deleteGameTestRepository, signal *deleteGameTestSignal) {
	t.Helper()
	if repository.stateReads != 1 || repository.impactReads != 1 || repository.scheduleCalls != 1 ||
		repository.runtimeCalls != 1 || repository.auditCalls != 1 || len(repository.replayWrites) != 1 || signal.calls != 1 {
		t.Fatalf("delete calls state=%d impact=%d schedule=%d runtime=%d audit=%d replay=%d signal=%d",
			repository.stateReads, repository.impactReads, repository.scheduleCalls, repository.runtimeCalls,
			repository.auditCalls, len(repository.replayWrites), signal.calls)
	}
}

func assertFreshDeleteReplay(t *testing.T, replay model.DeleteGameReplayWrite) {
	t.Helper()
	if replay.HTTPStatus != 202 || replay.ExpiresAtMS != 86_400_100 {
		t.Fatalf("stored delete replay = %+v", replay)
	}
}

func TestDeleteAdminGameRejectsStaleConfirmationBeforeSideEffects(t *testing.T) {
	repository := &deleteGameTestRepository{
		state:  model.DeleteGameState{Title: "Fixture", Status: "PUBLISHED", Version: 1},
		impact: model.DeleteGameImpact{ImpactDigest: "impact"},
	}
	service := New(repository, time.Now)
	request := validDeleteGameRequest()
	request.ConfirmTitle = "Other"

	_, err := service.DeleteAdminGame(t.Context(), request)
	if !errors.Is(err, model.ErrDeleteGameConfirmationMismatch) {
		t.Fatalf("error = %v", err)
	}
	if repository.impactReads != 0 || repository.scheduleCalls != 0 || repository.runtimeCalls != 0 ||
		repository.auditCalls != 0 || len(repository.replayWrites) != 0 {
		t.Fatalf("stale confirmation caused side effects: %+v", repository)
	}
}

func TestDeleteAdminGameReplaysDurableResponseBeforeReadingGame(t *testing.T) {
	repository := &deleteGameTestRepository{
		findReplay: true,
		replay: model.DeleteGameReplay{
			RequestDigest: "digest", HTTPStatus: 202, HeadersJSON: `{"ETag":"v2"}`, Body: []byte(`{}\n`),
		},
	}
	service := New(repository, time.Now)

	result, err := service.DeleteAdminGame(t.Context(), validDeleteGameRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replayed || result.Replay.HTTPStatus != 202 || repository.stateReads != 0 || len(repository.replayWrites) != 0 {
		t.Fatalf("unexpected replay result: %+v repository=%+v", result, repository)
	}
}
