package launch

import (
	"context"
	"errors"
	"testing"
)

func TestProductCreatorSignalsValidationOnlyAfterCommit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		creator, repository, _, command := productFixture(t)
		repository.before.Source.VariantStatus = "BLOCKED"
		repository.current = cloneProductSnapshot(t, repository.before)
		cause := errors.New("creation commit unavailable")
		if fail {
			repository.commitErr = cause
		}
		signals := 0
		creator.environment.ResumeValidation = func(ctx context.Context, id string) {
			signals++
			if repository.inTransaction || id == "" || ctx.Err() != nil {
				t.Fatal("validation resume occurred before commit or retained request cancellation")
			}
		}
		result, err := creator.Create(t.Context(), command)
		if fail {
			if !errors.Is(err, cause) || signals != 0 || result.Created.JobID != "" {
				t.Fatalf("commit failure signals=%d job=%q error=%v", signals, result.Created.JobID, err)
			}
		} else if err != nil || signals != 1 || result.Status != 202 {
			t.Fatalf("committed signals=%d status=%d error=%v", signals, result.Status, err)
		}
	}
}
