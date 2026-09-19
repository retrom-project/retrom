package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	model "retrom/internal/model/emulationstationimport"
)

func TestInterruptedReviewPreparationUsesExistingTransitions(t *testing.T) {
	cases := []struct {
		state     string
		retryable bool
		want      []string
	}{
		{"PENDING", false, []string{"COPYING", "VALIDATING"}},
		{"COPYING", false, []string{"VALIDATING"}},
		{"VALIDATING", false, nil},
		{"COMMIT_FAILED", true, []string{"PENDING", "COPYING", "VALIDATING"}},
		{"SOURCE_CHANGED", true, []string{"PENDING", "COPYING", "VALIDATING"}},
		{"READ_FAILED", true, []string{"PENDING", "COPYING", "VALIDATING"}},
	}
	for _, test := range cases {
		t.Run(test.state, func(t *testing.T) {
			got, err := ReviewPreparation(model.ExecutionReview{State: test.state, Retryable: test.retryable})
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("preparation=%v error=%v", got, err)
			}
		})
	}
	for _, state := range []string{"COMMIT_FAILED", "SOURCE_CHANGED", "READ_FAILED", "CANCELLED", "PUBLISHED"} {
		if _, err := ReviewPreparation(model.ExecutionReview{State: state}); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("state=%s error=%v", state, err)
		}
	}
}
