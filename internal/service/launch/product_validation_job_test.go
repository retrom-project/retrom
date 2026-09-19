package launch

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/launch"
)

type validationJobMemory struct {
	current      model.ValidationJob
	found        bool
	failure      error
	writeFailure error
	writes       []model.ValidationJobWrite
}

func (repository *validationJobMemory) Find(context.Context, string) (model.ValidationJob, bool, error) {
	return repository.current, repository.found, repository.failure
}

func (repository *validationJobMemory) Write(_ context.Context, plan model.ValidationJobWrite) error {
	if repository.writeFailure != nil {
		return repository.writeFailure
	}
	repository.writes = append(repository.writes, plan)
	return nil
}

func TestValidationSchedulerRetainsStorageFailures(t *testing.T) {
	cause := errors.New("validation storage unavailable")
	for _, repository := range []*validationJobMemory{{failure: cause}, {writeFailure: cause}} {
		result, err := validationSchedulerFixture(repository).Queue(t.Context(), model.ValidationInputs{GameVariantID: "variant"})
		if !errors.Is(err, cause) || result.JobID != "" || len(repository.writes) != 0 {
			t.Fatalf("result=%+v error=%v writes=%d", result, err, len(repository.writes))
		}
	}
}

func TestValidationSchedulerChecksExecutionIdentityBeforeWriting(t *testing.T) {
	cause := errors.New("execution entropy")
	repository := &validationJobMemory{}
	scheduler := validationSchedulerFixture(repository)
	calls := 0
	scheduler.environment.NewID = func() (string, error) {
		calls++
		if calls == 1 {
			return validationFixtureID, nil
		}
		return "", cause
	}
	result, err := scheduler.Queue(t.Context(), model.ValidationInputs{GameVariantID: "variant"})
	if !errors.Is(err, cause) || calls != 2 || result.JobID != "" || len(repository.writes) != 0 {
		t.Fatalf("result=%+v error=%v calls=%d writes=%d", result, err, calls, len(repository.writes))
	}
}

func TestValidationSchedulerChecksIdentityBeforeWriting(t *testing.T) {
	cause := errors.New("validation entropy")
	repository := &validationJobMemory{}
	scheduler := NewValidationScheduler(repository, ValidationEnvironment{Now: func() time.Time { return time.UnixMilli(100) }, NewID: func() (string, error) { return "", cause }})
	result, err := scheduler.Queue(t.Context(), model.ValidationInputs{GameVariantID: "variant", ValidationInputDigest: "digest"})
	if !errors.Is(err, cause) || result.JobID != "" || len(repository.writes) != 0 {
		t.Fatalf("result=%+v error=%v writes=%d", result, err, len(repository.writes))
	}
}
