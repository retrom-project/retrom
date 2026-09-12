package pegasusimport

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

type workflowMemory struct {
	before                   WorkflowSnapshot
	err, writeErr, commitErr error
	cancellation             *CancellationPlan
	retry                    *RetryPlan
}

func (m *workflowMemory) WithControl(_ context.Context, work func(WorkflowScope) error) error {
	if err := work(WorkflowScope{Read: m, Write: m}); err != nil {
		return err
	}
	return m.commitErr
}

func (m *workflowMemory) Current(context.Context, string) (WorkflowSnapshot, error) {
	return m.before, m.err
}

func (m *workflowMemory) Cancel(_ context.Context, plan CancellationPlan) error {
	m.cancellation = &plan
	m.before.Summary.State = plan.State
	m.before.JobState = plan.State
	m.before.JobVersion++
	return m.writeErr
}

func (m *workflowMemory) Retry(_ context.Context, plan RetryPlan) error {
	m.retry = &plan
	m.before.Summary.State = "QUEUED"
	return m.writeErr
}

func workflowFixture() *workflowMemory {
	job := "job"
	return &workflowMemory{
		before: WorkflowSnapshot{
			Summary:        Summary{ID: "import", Version: 4, State: "PARTIAL_FAILURE", Retryable: true, ImportJobID: &job},
			JobState:       "SUCCEEDED",
			JobVersion:     3,
			Execution:      1,
			RetryableItems: 1,
		},
	}
}

func TestWorkflowRetryUsesNextExecutionAndCurrentActor(t *testing.T) {
	t.Parallel()
	m := workflowFixture()
	value, err := NewWorkflowControl(m, func() time.Time { return time.UnixMilli(10) }).Retry(
		t.Context(),
		"import",
		4,
		"actor",
	)
	if err != nil {
		t.Fatal(err)
	}
	if value.State != "QUEUED" || m.retry == nil {
		t.Fatalf("retry: %#v, plan=%#v", value, m.retry)
	}
	if m.retry.Execution != 2 || m.retry.Before.JobVersion != 3 || m.retry.ActorID != "actor" || m.retry.NowMS != 10 || m.retry.ExecutionID == "" || m.retry.AuditID == "" {
		t.Fatalf("retry plan: %#v", m.retry)
	}
}

func TestWorkflowRetryRejectsStaleBusyAndExhaustedIdentity(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"version", "active", "items", "execution", "job"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			m := workflowFixture()
			switch reason {
			case "version":
				m.before.Summary.Version++
			case "active":
				m.before.OtherActive = true
			case "items":
				m.before.RetryableItems = 0
			case "execution":
				m.before.Execution = math.MaxInt64
			case "job":
				m.before.JobState = "RUNNING"
			}
			value, err := NewWorkflowControl(m, time.Now).Retry(t.Context(), "import", 4, "actor")
			if !errors.Is(err, ErrNotRetryable) || value.ID != "" || m.retry != nil {
				t.Fatalf("invalid retry: %#v, %v", value, err)
			}
		})
	}
}

func TestWorkflowCancellationDistinguishesClaimedAndQueuedWork(t *testing.T) {
	t.Parallel()
	for _, jobState := range []string{"QUEUED", "RUNNING"} {
		t.Run(jobState, func(t *testing.T) {
			t.Parallel()
			m := workflowFixture()
			m.before.Summary.State = "QUEUED"
			m.before.JobState = jobState
			_, pending, err := NewWorkflowControl(m, func() time.Time { return time.UnixMilli(10) }).Cancel(
				t.Context(),
				"import",
				4,
				"  Stop  ",
				"actor",
			)
			if err != nil {
				t.Fatal(err)
			}
			if m.cancellation == nil || m.cancellation.Reason != "Stop" || m.cancellation.ActorID != "actor" {
				t.Fatalf("cancel plan: %#v", m.cancellation)
			}
			if pending != (jobState == "RUNNING") || m.cancellation.Pending != pending {
				t.Fatalf("claim state %s: pending=%v", jobState, pending)
			}
			if pending {
				if m.cancellation.State != "CANCEL_REQUESTED" || m.cancellation.CompletedAtMS != nil {
					t.Fatalf("pending cancellation: %#v", m.cancellation)
				}
			} else if m.cancellation.State != "CANCELLED" || m.cancellation.CompletedAtMS == nil {
				t.Fatalf("queued cancellation: %#v", m.cancellation)
			}
		})
	}
}

func TestWorkflowCancellationRejectsInvalidReason(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{" ", strings.Repeat("停", 501)} {
		m := workflowFixture()
		_, _, err := NewWorkflowControl(m, time.Now).Cancel(t.Context(), "import", 4, reason, "actor")
		if !errors.Is(err, ErrNotCancellable) || m.cancellation != nil {
			t.Fatalf("invalid reason: %v", err)
		}
	}
}

func TestWorkflowFailurePreservesCauseAndHidesPartialResults(t *testing.T) {
	t.Parallel()
	cause := errors.New("storage failure")
	for _, phase := range []string{"read", "write", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			m := workflowFixture()
			switch phase {
			case "read":
				m.err = cause
			case "write":
				m.writeErr = cause
			case "commit":
				m.commitErr = cause
			}
			value, err := NewWorkflowControl(m, time.Now).Retry(t.Context(), "import", 4, "actor")
			if !errors.Is(err, cause) || value.ID != "" {
				t.Fatalf("partial retry: %#v, %v", value, err)
			}
		})
	}
}

func TestWorkflowCancellationPreservesFailuresAndRejectsStaleState(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"read", "write", "commit", "version", "job", "state", "missing_job"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			m := workflowFixture()
			m.before.Summary.State = "QUEUED"
			m.before.JobState = "QUEUED"
			cause := errors.New("storage failure")
			want := cause
			switch phase {
			case "read":
				m.err = cause
			case "write":
				m.writeErr = cause
			case "commit":
				m.commitErr = cause
			default:
				want = ErrNotCancellable
				invalidateCancellation(m, phase)
			}
			value, pending, err := NewWorkflowControl(m, time.Now).Cancel(t.Context(), "import", 4, "Stop", "actor")
			if !errors.Is(err, want) || value.ID != "" || pending {
				t.Fatalf("invalid cancellation: %#v %v pending=%v", value, err, pending)
			}
		})
	}
}

func invalidateCancellation(m *workflowMemory, phase string) {
	switch phase {
	case "version":
		m.before.Summary.Version++
	case "job":
		m.before.JobState = "SUCCEEDED"
	case "state":
		m.before.Summary.State = "COMPLETED"
	case "missing_job":
		m.before.Summary.ImportJobID = nil
	}
}

func TestWorkflowCancellationAllowsFiveHundredUnicodeCharacters(t *testing.T) {
	t.Parallel()
	m := workflowFixture()
	m.before.Summary.State = "QUEUED"
	m.before.JobState = "QUEUED"
	reason := strings.Repeat("停", 500)
	if _, _, err := NewWorkflowControl(m, time.Now).Cancel(t.Context(), "import", 4, reason, "actor"); err != nil {
		t.Fatal(err)
	}
	if m.cancellation == nil || m.cancellation.Reason != reason {
		t.Fatalf("reason changed: %#v", m.cancellation)
	}
}
