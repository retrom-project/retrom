package gamecontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

type budgetRepository struct {
	Repository
	input StoredInput
	claim Claim
}

func (repository *budgetRepository) WithRead(_ context.Context, work func(ReadScope) error) error {
	return work(ReadScope{Inputs: repository})
}

func (repository *budgetRepository) Input(context.Context, string, int64) (StoredInput, error) {
	return repository.input, nil
}

func (repository *budgetRepository) WithWrite(_ context.Context, work func(WriteScope) error) error {
	return work(WriteScope{Leases: budgetLeases{repository: repository}})
}

type budgetLeases struct {
	LeaseRecords
	repository *budgetRepository
}

func (leases budgetLeases) Claim(_ context.Context, claim Claim) (bool, error) {
	leases.repository.claim = claim
	return false, nil
}

func TestReplacementKeepsSixHourExecutionBudget(t *testing.T) {
	input, err := json.Marshal(inputEnvelope{
		SchemaVersion: 1, Kind: "GAME_CONTENT_REPLACE", Scope: inputScope{Type: "GAME", ID: "game"},
		ExecutionID: "01980000-0000-7000-8000-000000000001", Inputs: JobSnapshot{GameID: "game"},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(input)
	repository := &budgetRepository{input: StoredInput{Contents: input, Digest: hex.EncodeToString(digest[:])}}
	service := New(repository, func() time.Time { return time.UnixMilli(100) })
	if err := service.Run(t.Context(), "job", 1); err != nil {
		t.Fatal(err)
	}
	if got := repository.claim.Deadline - repository.claim.Now; got != int64(6*time.Hour/time.Millisecond) {
		t.Fatalf("replacement execution budget shortened to %d ms", got)
	}
}
