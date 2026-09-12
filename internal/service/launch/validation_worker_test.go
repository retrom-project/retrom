package launch

import (
	"errors"
	"testing"
)

func TestValidationWorkerIdentityIsChecked(t *testing.T) {
	cause := errors.New("entropy failed")
	if _, err := checkedValidationWorkerID(func() (string, error) { return "", cause }); !errors.Is(err, cause) {
		t.Fatalf("lost identity cause: %v", err)
	}
	for _, id := range []string{"", "in-process", "00000000-0000-4000-8000-000000000001"} {
		if _, err := checkedValidationWorkerID(func() (string, error) { return id, nil }); err == nil {
			t.Fatalf("accepted worker identity %q", id)
		}
	}
}

func TestValidationWorkerCannotUseExpiredOrDifferentAuthority(t *testing.T) {
	claim := ValidationClaim{Job: ValidationWork{ID: "job", ExecutionNo: 1, Attempt: 1, WorkerID: "worker", InputDigest: "digest"}}
	deadline, lease := int64(100), int64(90)
	claim.Job.DeadlineMS = &deadline
	current := claim.Job
	current.State = "RUNNING"
	current.DeadlineMS = &deadline
	current.LeaseMS = &lease
	if !ownsValidationWork(current, claim, 89) {
		t.Fatal("valid owner rejected")
	}
	if ownsValidationWork(current, claim, 90) {
		t.Fatal("expired lease accepted")
	}
	current.WorkerID = "replacement"
	if ownsValidationWork(current, claim, 89) {
		t.Fatal("replacement owner accepted")
	}
}
