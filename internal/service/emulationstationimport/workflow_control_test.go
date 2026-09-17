package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type workflowMemory struct {
	current     model.WorkflowSnapshot
	source      model.FrozenSourceSnapshot
	targets     bool
	failure     error
	stage       string
	cancel      *model.CancellationPlan
	retry       *model.RetryPlan
	writeScopes int
	afterVerify func()
}

func (m *workflowMemory) InspectRetry(_ context.Context, _ string) (model.RetrySnapshot, error) {
	if m.stage == "read" {
		return model.RetrySnapshot{}, m.failure
	}
	return model.RetrySnapshot{
		WorkflowSnapshot:     m.current,
		FrozenSourceSnapshot: m.source,
		TargetsValid:         m.targets,
	}, nil
}

func (m *workflowMemory) CommitCancelWorkflow(
	_ context.Context, cmd model.CancelWorkflowCommand,
) (model.WorkflowSnapshot, bool, error) {
	m.writeScopes++
	if m.afterVerify != nil {
		m.afterVerify()
	}
	if m.stage == "read" {
		return model.WorkflowSnapshot{}, false, m.failure
	}
	version := cmd.Version
	if cmd.Job != nil {
		if !model.MatchesCancellationJob(m.current, cmd.Job.JobID, cmd.Job.Kind, cmd.Job.ScopeID) {
			return model.WorkflowSnapshot{}, false, model.ErrNotCancellable
		}
		if cmd.Job.ExpectedVersion < 1 || cmd.Job.ExpectedVersion != m.current.JobVersion {
			return model.WorkflowSnapshot{}, false, model.ErrVersionConflict
		}
		version = m.current.Summary.Version
	}
	if !model.CanCancelWorkflow(m.current, version) {
		return model.WorkflowSnapshot{}, false, model.ErrNotCancellable
	}
	pending := m.current.JobState == "RUNNING"
	state := "CANCELLED"
	if pending {
		state = "CANCEL_REQUESTED"
	}
	plan := model.CancellationPlan{
		Before: m.current, Reason: cmd.Reason, ActorID: cmd.ActorID, AuditID: cmd.AuditID,
		NowMS: cmd.NowMS, State: state, Pending: pending,
	}
	if !pending {
		plan.CompletedAtMS = &cmd.NowMS
	}
	if m.stage == "write" {
		return model.WorkflowSnapshot{}, false, m.failure
	}
	m.cancel = &plan
	m.current.Summary.State = plan.State
	if m.stage == "response" {
		return model.WorkflowSnapshot{}, false, m.failure
	}
	if m.stage == "commit" {
		return model.WorkflowSnapshot{}, false, m.failure
	}
	return m.current, pending, nil
}

func (m *workflowMemory) CommitRetryWorkflow(
	_ context.Context, cmd model.RetryWorkflowCommand,
) (model.Summary, error) {
	m.writeScopes++
	if m.afterVerify != nil {
		m.afterVerify()
	}
	if m.stage == "read" {
		return model.Summary{}, m.failure
	}
	current := model.RetrySnapshot{
		WorkflowSnapshot:     m.current,
		FrozenSourceSnapshot: m.source,
		TargetsValid:         m.targets,
	}
	if err := model.ValidateRetryEligibility(current, cmd.Version); err != nil {
		return model.Summary{}, err
	}
	if !model.SameRetryExecution(cmd.Plan.Before, current) {
		return model.Summary{}, model.ErrNotRetryable
	}
	if !model.SameFrozenSource(
		cmd.Plan.Before.Summary, current.Summary,
		cmd.Plan.Before.FrozenSourceSnapshot, current.FrozenSourceSnapshot,
	) {
		return model.Summary{}, model.ErrSourceChanged
	}
	plan := cmd.Plan
	plan.Before = current
	if m.stage == "write" {
		return model.Summary{}, m.failure
	}
	m.retry = &plan
	m.current.Summary.State = "QUEUED"
	if m.stage == "response" || m.stage == "commit" {
		return model.Summary{}, m.failure
	}
	return m.current.Summary, nil
}

func workflowFixture() (*workflowMemory, *startSources) {
	start, sources := startApplicationFixture()
	summary := start.snapshot.Summary
	summary.State = "PARTIAL_FAILURE"
	summary.Retryable = true
	summary.ImportJobID = stringPointer("job")
	return &workflowMemory{
		current: model.WorkflowSnapshot{
			Summary: summary, JobState: "SUCCEEDED", JobVersion: 3, Execution: 1, RetryableItems: 2,
		},
		source:  start.snapshot.FrozenSourceSnapshot,
		targets: true,
	}, sources
}

func TestWorkflowRetryVerifiesSourceBeforeNewExecution(t *testing.T) {
	t.Parallel()
	m, sources := workflowFixture()
	m.afterVerify = func() {
		if !sources.verified {
			t.Fatal("write scope opened before source verification")
		}
	}
	result, err := NewWorkflowControl(m, sources, func() time.Time { return time.UnixMilli(1000) }).Retry(
		t.Context(), "import", 4, "editor",
	)
	if err != nil || result.State != "QUEUED" || m.retry == nil {
		t.Fatalf("retry=%#v error=%v", result, err)
	}
	if m.retry.Execution != 2 || m.retry.ActorID != "editor" || m.retry.ExecutionID == "" || m.retry.AuditID == "" {
		t.Fatalf("retry plan=%#v", m.retry)
	}
}

func TestWorkflowCancelUsesJobAndPlanStateWithoutSource(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING"} {
		m, sources := workflowFixture()
		m.current.Summary.State = state
		m.current.JobState = state
		sources.err = errors.New("source unavailable")
		result, pending, err := NewWorkflowControl(m, sources, func() time.Time { return time.UnixMilli(10) }).Cancel(
			t.Context(), "import", 4, "  Stop  ", "editor",
		)
		want := "CANCELLED"
		if state == "RUNNING" {
			want = "CANCEL_REQUESTED"
		}
		if err != nil || result.State != want || pending != (state == "RUNNING") || sources.verified || m.cancel == nil || m.cancel.Reason != "Stop" {
			t.Fatalf("cancel=%#v pending=%v error=%v", result, pending, err)
		}
	}
}

func TestWorkflowFailuresNeverReturnPartialResponses(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"cancel", "retry"} {
		for _, stage := range []string{"read", "write", "response", "commit"} {
			m, sources := workflowFixture()
			m.stage = stage
			m.failure = errors.New("workflow storage failed")
			service := NewWorkflowControl(m, sources, func() time.Time { return time.UnixMilli(10) })
			var result model.Summary
			var err error
			var pending bool
			if operation == "cancel" {
				m.current.Summary.State = "RUNNING"
				m.current.JobState = "RUNNING"
				result, pending, err = service.Cancel(t.Context(), "import", 4, "Stop", "editor")
			} else {
				result, err = service.Retry(t.Context(), "import", 4, "editor")
			}
			if result.ID != "" || pending || !errors.Is(err, m.failure) {
				t.Fatalf("%s %s result=%#v pending=%v error=%v", operation, stage, result, pending, err)
			}
		}
	}
}
