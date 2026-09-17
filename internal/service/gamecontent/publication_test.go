package gamecontent

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/gamecontent"
)

type workflowRepository struct {
	model.Repository
	publishResult model.PublishResult
	publishErr    error
	settleResult  model.SettleFailureResult
	settleErr     error
	committed     bool
	lateError     error
}

func (repository *workflowRepository) CommitPublish(
	_ context.Context, _ model.PublishCommand,
) (model.PublishResult, error) {
	if repository.publishErr != nil {
		return model.PublishResult{}, repository.publishErr
	}
	if repository.lateError != nil {
		return model.PublishResult{}, repository.lateError
	}
	repository.committed = true
	return repository.publishResult, nil
}

func (repository *workflowRepository) CommitSettleFailure(
	_ context.Context, _ model.SettleFailureCommand,
) (model.SettleFailureResult, error) {
	if repository.settleErr != nil {
		return model.SettleFailureResult{}, repository.settleErr
	}
	repository.committed = true
	return repository.settleResult, nil
}

type commitSignal struct {
	t          *testing.T
	repository *workflowRepository
	calls      int
}

func (signal *commitSignal) Signal() {
	signal.calls++
}

func TestPublicationRejectsChangedContentBeforeRetirement(t *testing.T) {
	for _, test := range []struct {
		name string
		code string
	}{
		{name: "lost lease", code: "GAME_CONTENT_EXECUTION_LOST"},
		{name: "game changed", code: "GAME_CONTENT_CHANGED"},
		{name: "same bytes", code: "GAME_CONTENT_UNCHANGED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			publishErr := errors.New(test.code)
			repository := &workflowRepository{publishErr: publishErr}
			service := New(repository, func() time.Time { return time.UnixMilli(100) })
			err := service.publish(t.Context(), model.Claim{}, model.JobSnapshot{}, model.PreparedReplacement{})
			if err == nil {
				t.Fatal("expected error")
			}
			if repository.committed {
				t.Fatal("rejected publication committed")
			}
		})
	}
}

func TestFailureSettlementHonorsCancellationAndOwnership(t *testing.T) {
	for _, test := range []struct {
		state         string
		changed       bool
		signalRelease bool
		cancelled     bool
	}{
		{"CANCEL_REQUESTED", true, true, true},
		{"RUNNING", true, false, false},
		{"CANCELLED", false, false, false},
		{"SUCCEEDED", false, false, false},
	} {
		t.Run(test.state, func(t *testing.T) {
			repository := &workflowRepository{
				settleResult: model.SettleFailureResult{
					Changed:       test.changed,
					Retryable:     !test.signalRelease && test.changed,
					SignalRelease: test.signalRelease,
				},
			}
			signal := &commitSignal{t: t, repository: repository}
			service := New(repository, func() time.Time { return time.UnixMilli(100) }).WithPayloadRelease(signal)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := service.settleFailure(ctx, model.Claim{WorkerID: "owner"}, model.JobSnapshot{}, context.Canceled); err != nil {
				t.Fatal(err)
			}
			if test.signalRelease && signal.calls != 1 {
				t.Fatalf("expected signal, got calls=%d", signal.calls)
			}
			if !test.signalRelease && signal.calls != 0 {
				t.Fatalf("unexpected signal, got calls=%d", signal.calls)
			}
		})
	}
}

func TestFailureSettlementPreservesLateErrorWithoutSignalling(t *testing.T) {
	repository := &workflowRepository{settleErr: context.DeadlineExceeded}
	signal := &commitSignal{t: t, repository: repository}
	service := New(repository, func() time.Time { return time.UnixMilli(100) }).WithPayloadRelease(signal)
	cause := &replacementValidationError{code: "GAME_CONTENT_CHANGED"}
	err := service.settleFailure(t.Context(), model.Claim{}, model.JobSnapshot{}, cause)
	if !errors.Is(err, cause) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost failure: %v", err)
	}
	if signal.calls != 0 || repository.committed {
		t.Fatal("failed transaction published cleanup signal")
	}
}
