package emulationstationimport

import (
	"context"
	"errors"
	model "retrom/internal/model/emulationstationimport"
	"testing"
	"time"
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

func (m *workflowMemory) Current(context.Context, string) (model.WorkflowSnapshot, error) {
	if m.stage == "read" || m.stage == "response" && (m.cancel != nil || m.retry != nil) {
		return model.WorkflowSnapshot{}, m.failure
	}
	return m.current, nil
}

func (m *workflowMemory) RetryCurrent(ctx context.Context, id string) (model.RetrySnapshot, error) {
	value, err := m.Current(ctx, id)
	return model.RetrySnapshot{WorkflowSnapshot: value, FrozenSourceSnapshot: m.source, TargetsValid: m.targets}, err
}

func (m *workflowMemory) InspectRetry(ctx context.Context, id string) (model.RetrySnapshot, error) {
	return m.RetryCurrent(ctx, id)
}

func (m *workflowMemory) WithControl(_ context.Context, work func(model.WorkflowScope) error) error {
	m.writeScopes++
	if m.afterVerify != nil {
		m.afterVerify()
	}
	if err := work(model.WorkflowScope{Payload: emptyPayloadScope(), Read: m, Write: m}); err != nil {
		return err
	}
	if m.stage == "commit" {
		return m.failure
	}
	return nil
}

func (m *workflowMemory) Cancel(_ context.Context, plan model.CancellationPlan) error {
	m.cancel = &plan
	m.current.Summary.State = plan.State
	if m.stage == "write" {
		return m.failure
	}
	return nil
}

func (m *workflowMemory) Retry(_ context.Context, plan model.RetryPlan) error {
	m.retry = &plan
	m.current.Summary.State = "QUEUED"
	if m.stage == "write" {
		return m.failure
	}
	return nil
}

func workflowFixture() (*workflowMemory, *startSources) {
	start, sources := startApplicationFixture()
	summary := start.snapshot.Summary
	summary.State = "PARTIAL_FAILURE"
	summary.Retryable = true
	summary.ImportJobID = stringPointer("job")
	return &workflowMemory{current: model.WorkflowSnapshot{Summary: summary, JobState: "SUCCEEDED", JobVersion: 3, Execution: 1, RetryableItems: 2}, source: start.snapshot.FrozenSourceSnapshot, targets: true}, sources
}

func TestWorkflowRetryVerifiesSourceBeforeNewExecution(t *testing.T) {
	t.Parallel()
	m, sources := workflowFixture()
	m.afterVerify = func() {
		if !sources.verified {
			t.Fatal("write scope opened before source verification")
		}
	}
	result, err := NewWorkflowControl(m, sources, func() time.Time { return time.UnixMilli(1000) }).Retry(t.Context(), "import", 4, "editor")
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
		result, pending, err := NewWorkflowControl(m, sources, func() time.Time { return time.UnixMilli(10) }).Cancel(t.Context(), "import", 4, "  Stop  ", "editor")
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
