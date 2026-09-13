package emulationstationimport

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

var errIdentityEntropy = errors.New("identity entropy unavailable")

type identityEntropy struct{ remaining int }

func (reader *identityEntropy) Read(value []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, errIdentityEntropy
	}
	reader.remaining--
	for i := range value {
		value[i] = byte(i + reader.remaining)
	}
	return len(value), nil
}

// Sequential: restore UUID's process-wide reader before parallel tests resume.
func TestCreationChecksEveryIdentityBeforeOpeningTransaction(t *testing.T) {
	for allowed := range 4 {
		repo := &creationMemory{}
		value, err := func() (Summary, error) {
			uuid.SetRand(&identityEntropy{remaining: allowed})
			defer uuid.SetRand(nil)
			return NewCreation(repo, &creationSource{}, time.Now).Create(t.Context(), CreateRequest{}, "actor")
		}()
		if !errors.Is(err, errIdentityEntropy) || value.ID != "" || repo.scopes != 0 {
			t.Fatalf("identity %d produced value=%#v error=%v scopes=%d", allowed, value, err, repo.scopes)
		}
	}
}

func TestPlanDeletionChecksAuditIdentityBeforeMutation(t *testing.T) {
	repo := &planLifecycleMemory{summary: Summary{ID: "plan", Version: 1, State: "EXPIRED", CreatedBy: CreatedBy{ID: "actor"}}}
	err := func() error {
		uuid.SetRand(&identityEntropy{})
		defer uuid.SetRand(nil)
		return NewPlanLifecycle(repo, time.Now).Delete(t.Context(), "plan", 1, "actor")
	}()
	if !errors.Is(err, errIdentityEntropy) || repo.deleted != nil {
		t.Fatalf("audit identity error=%v deletion=%#v", err, repo.deleted)
	}
}
