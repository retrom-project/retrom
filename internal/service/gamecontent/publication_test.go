package gamecontent

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/gamecontent"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
)

type workflowRepository struct {
	model.Repository

	scope     model.WriteScope
	lateError error
	committed bool
}

func (repository *workflowRepository) WithWrite(ctx context.Context, work func(model.WriteScope) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := work(repository.scope); err != nil {
		return err
	}
	if repository.lateError != nil {
		return repository.lateError
	}
	repository.committed = true
	return nil
}

type workflowContent struct {
	model.Reader

	binding  model.Binding
	identity []model.IdentityFile
}

func (content workflowContent) Binding(context.Context, string) (model.Binding, error) {
	return content.binding, nil
}

func (content workflowContent) Identity(context.Context, string) ([]model.IdentityFile, error) {
	return content.identity, nil
}

type workflowLeases struct {
	model.LeaseRecords

	current bool
	state   string
}

func (leases workflowLeases) Current(context.Context, model.Claim, int64) (bool, error) {
	return leases.current, nil
}

func (leases workflowLeases) State(context.Context, model.Claim) (string, error) {
	return leases.state, nil
}

func TestPublicationRejectsChangedContentBeforeRetirement(t *testing.T) {
	dat := "changed"
	for _, test := range []struct {
		name     string
		current  bool
		binding  model.Binding
		identity []model.IdentityFile
		code     string
	}{
		{name: "lost lease", code: "GAME_CONTENT_EXECUTION_LOST"},
		{name: "game changed", current: true, binding: model.Binding{Version: 2}, code: "GAME_CONTENT_CHANGED"},
		{name: "DAT changed", current: true, binding: model.Binding{DATID: &dat}, code: "GAME_CONTENT_CHANGED"},
		{name: "same bytes", current: true, identity: []model.IdentityFile{{Role: "CONTENT", SHA256: "same"}}, code: "GAME_CONTENT_UNCHANGED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &workflowRepository{scope: model.WriteScope{
				ReadScope: model.ReadScope{Content: workflowContent{binding: test.binding, identity: test.identity}},
				Leases:    workflowLeases{current: test.current},
			}}
			service := New(repository, func() time.Time { return time.UnixMilli(100) })
			err := service.publish(t.Context(), model.Claim{}, model.JobSnapshot{}, model.PreparedReplacement{Files: []model.ReplacementFile{{Role: "CONTENT", SHA256: "same"}}})
			if test.code == "GAME_CONTENT_EXECUTION_LOST" {
				if !errors.Is(err, model.ErrExecutionLost) {
					t.Fatalf("lost ownership: %v", err)
				}
			} else {
				var validation *replacementValidationError
				if !errors.As(err, &validation) || validation.code != test.code {
					t.Fatalf("validation: %v", err)
				}
			}
			if repository.committed {
				t.Fatal("rejected publication committed")
			}
		})
	}
}

type failureJobs struct {
	model.JobWriter

	outcome model.Outcome
	calls   int
}

func (jobs *failureJobs) Fail(_ context.Context, outcome model.Outcome) (bool, error) {
	jobs.outcome = outcome
	jobs.calls++
	return true, nil
}

type failureRetirements struct {
	payloadreleasemodel.SchedulingScope

	releases int
}

func (records *failureRetirements) Consumption(context.Context, string) (payloadreleasemodel.Consumption, error) {
	return payloadreleasemodel.Consumption{Version: 1}, nil
}

func (records *failureRetirements) CreateJob(context.Context, payloadreleasemodel.ScheduledJob) error {
	records.releases++
	return nil
}

type failureConsumption struct{ model.RetirementReader }

func (failureConsumption) Consumption(context.Context, string) (string, error) {
	return "consumption", nil
}

type commitSignal struct {
	t          *testing.T
	repository *workflowRepository
	calls      int
}

func (signal *commitSignal) Signal() {
	if !signal.repository.committed {
		signal.t.Fatal("release signaled before commit")
	}
	signal.calls++
}

func TestFailureSettlementHonorsCancellationAndOwnership(t *testing.T) {
	for _, test := range []struct {
		state           string
		calls, releases int
		cancelled       bool
	}{
		{"CANCEL_REQUESTED", 1, 1, true},
		{"RUNNING", 1, 0, false},
		{"CANCELLED", 0, 0, false},
		{"SUCCEEDED", 0, 0, false},
		{"", 0, 0, false},
	} {
		t.Run(test.state, func(t *testing.T) {
			jobs, retirements := &failureJobs{}, &failureRetirements{}
			repository := &workflowRepository{scope: model.WriteScope{Leases: workflowLeases{state: test.state}, Jobs: jobs, Retirements: model.RetirementScope{Read: failureConsumption{}, Payload: retirements}}}
			signal := &commitSignal{t: t, repository: repository}
			service := New(repository, func() time.Time { return time.UnixMilli(100) }).WithPayloadRelease(signal)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := service.settleFailure(ctx, model.Claim{WorkerID: "owner"}, model.JobSnapshot{}, context.Canceled); err != nil {
				t.Fatal(err)
			}
			if jobs.calls != test.calls || retirements.releases != test.releases || signal.calls != test.releases {
				t.Fatalf("failure calls=%d releases=%d signals=%d", jobs.calls, retirements.releases, signal.calls)
			}
			if jobs.outcome.Cancelled != test.cancelled {
				t.Fatalf("cancel acknowledgement: %+v", jobs.outcome)
			}
			if test.cancelled && jobs.outcome.Retryable {
				t.Fatal("cancelled execution marked retryable")
			}
		})
	}
}

func TestFailureSettlementPreservesLateErrorWithoutSignalling(t *testing.T) {
	jobs, retirements := &failureJobs{}, &failureRetirements{}
	repository := &workflowRepository{scope: model.WriteScope{Leases: workflowLeases{state: "RUNNING"}, Jobs: jobs, Retirements: model.RetirementScope{Read: failureConsumption{}, Payload: retirements}}, lateError: context.DeadlineExceeded}
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
	if jobs.calls != 1 || retirements.releases != 1 {
		t.Fatal("late failure did not exercise terminal writes")
	}
}
