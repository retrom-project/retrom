package emulationstationimport

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"

	"github.com/google/uuid"
)

func TestWorkflowCancelChecksVersionStateAndUnicodeReason(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"empty", "long", "500 runes", "stale", "zero", "overflow", "job overflow", "execution zero", "scan", "job finished"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			m, sources := workflowFixture()
			m.current.Summary.State = "QUEUED"
			m.current.JobState = "QUEUED"
			reason, version := "Stop", int64(4)
			reason, version = changeCancellationBoundary(m, kind, reason, version)
			_, pending, err := NewWorkflowControl(m, sources, time.Now).Cancel(t.Context(), "import", version, reason, "actor")
			valid := kind == "500 runes"
			if pending || valid != (err == nil) || !valid && !errors.Is(err, model.ErrNotCancellable) || !valid && m.cancel != nil || sources.verified {
				t.Fatalf("%s pending=%v error=%v", kind, pending, err)
			}
		})
	}
}

func TestWorkflowRetryChecksTerminalAndNumericBoundaries(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"plan overflow", "job overflow", "execution overflow", "execution zero", "stale", "not retryable", "no items", "running job", "cancelled plan", "active", "target", "expired mapping"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			m, sources := workflowFixture()
			version := int64(4)
			want := model.ErrNotRetryable
			version, want = changeRetryBoundary(m, kind, version, want)
			result, err := NewWorkflowControl(m, sources, func() time.Time { return time.UnixMilli(1000) }).Retry(t.Context(), "import", version, "actor")
			valid := kind == "expired mapping"
			if valid != (err == nil) || !valid && (!errors.Is(err, want) || result.ID != "" || sources.verified || m.writeScopes != 0) {
				t.Fatalf("%s result=%#v error=%v", kind, result, err)
			}
		})
	}
}

func TestWorkflowRetryRevalidatesExecutionAndSourceAfterIO(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"version", "job version", "execution", "job id", "mapping", "state", "target", "root", "year", "evidence", "reread"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			m, sources := workflowFixture()
			want := model.ErrNotRetryable
			sources.verify = func() {
				want = changeRetryAfterIO(m, field)
			}
			result, err := NewWorkflowControl(m, sources, time.Now).Retry(t.Context(), "import", 4, "actor")
			if !errors.Is(err, want) || result.ID != "" || m.retry != nil {
				t.Fatalf("%s result=%#v error=%v", field, result, err)
			}
		})
	}
}

func TestWorkflowChecksEveryIdentityFailure(t *testing.T) {
	for _, operation := range []string{"cancel", "retry"} {
		total := 1
		if operation == "retry" {
			total = 2
		}
		for allowed := range total {
			m, sources := workflowFixture()
			err := func() error {
				uuid.SetRand(&identityEntropy{remaining: allowed})
				defer uuid.SetRand(nil)
				service := NewWorkflowControl(m, sources, time.Now)
				if operation == "cancel" {
					m.current.Summary.State = "QUEUED"
					m.current.JobState = "QUEUED"
					_, _, err := service.Cancel(t.Context(), "import", 4, "Stop", "actor")
					return err
				}
				_, err := service.Retry(t.Context(), "import", 4, "actor")
				return err
			}()
			if !errors.Is(err, errIdentityEntropy) || m.cancel != nil || m.retry != nil {
				t.Fatalf("%s identity %d error=%v", operation, allowed, err)
			}
		}
	}
}

func changeCancellationBoundary(m *workflowMemory, kind, reason string, version int64) (string, int64) {
	switch kind {
	case "empty":
		reason = " \t\n"
	case "long":
		reason = strings.Repeat("停", 501)
	case "500 runes":
		reason = strings.Repeat("停", 500)
	case "stale":
		version = 3
	case "zero":
		version = 0
		m.current.Summary.Version = 0
	case "overflow":
		version = math.MaxInt64
		m.current.Summary.Version = version
	case "job overflow":
		m.current.JobVersion = math.MaxInt64
	case "execution zero":
		m.current.Execution = 0
	case "scan":
		m.current.Summary.State = "SCANNING"
		m.current.Summary.ImportJobID = nil
	case "job finished":
		m.current.JobState = "SUCCEEDED"
	}
	return reason, version
}

func changeRetryBoundary(m *workflowMemory, kind string, version int64, want error) (int64, error) {
	switch kind {
	case "plan overflow":
		version = math.MaxInt64
		m.current.Summary.Version = version
	case "job overflow":
		m.current.JobVersion = math.MaxInt64
	case "execution overflow":
		m.current.Execution = math.MaxInt64
	case "execution zero":
		m.current.Execution = 0
	case "stale":
		version = 3
	case "not retryable":
		m.current.Summary.Retryable = false
	case "no items":
		m.current.RetryableItems = 0
	case "running job":
		m.current.JobState = "RUNNING"
	case "cancelled plan":
		m.current.Summary.State = "CANCELLED"
	case "active":
		m.current.OtherActive = true
		want = model.ErrActive
	case "target":
		m.targets = false
		want = model.ErrMappingTargetChanged
	case "expired mapping":
		m.current.Summary.ExpiresAtMS = 1
	}
	return version, want
}

func changeRetryAfterIO(m *workflowMemory, field string) error {
	want := model.ErrNotRetryable
	switch field {
	case "version":
		m.current.Summary.Version++
	case "job version":
		m.current.JobVersion++
	case "execution":
		m.current.Execution++
	case "job id":
		m.current.Summary.ImportJobID = stringPointer("replacement-job")
	case "mapping":
		m.current.Summary.MappingVersion++
	case "state":
		m.current.JobState = "RUNNING"
	case "target":
		m.targets = false
		want = model.ErrMappingTargetChanged
	case "root":
		m.source.RootConfigDigest = "changed"
		want = model.ErrSourceChanged
	case "year":
		m.source.ReleaseYearMax++
		want = model.ErrSourceChanged
	case "evidence":
		m.source.Gamelists = append([]model.GamelistEvidence(nil), m.source.Gamelists...)
		m.source.Gamelists[0].SizeBytes++
		want = model.ErrSourceChanged
	case "reread":
		m.failure = errors.New("final read failed")
		m.stage = "read"
		want = m.failure
	}
	return want
}
