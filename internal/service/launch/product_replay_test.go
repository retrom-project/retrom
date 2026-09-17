package launch

import (
	"bytes"
	"errors"
	model "retrom/internal/model/launch"
	"testing"
)

func TestProductCreatorRechecksReceiptBeforeFinalAuthority(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	winner, err := productReceipt(model.Created{LaunchID: validationFixtureID, PlayURL: "/play/" + validationFixtureID, Warnings: []string{}}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	winner.Digest = command.Digest
	repository.finalReceipt = &winner
	repository.currentErr = errors.New("must not read changed inputs after replay")
	result, err := creator.Create(t.Context(), command)
	if err != nil || result.Created.LaunchID != validationFixtureID || !result.Replayed || !bytes.Equal(result.Body, winner.Body) || len(repository.writes) != 0 {
		t.Fatalf("launch=%q error=%v replay=%v writes=%d", result.Created.LaunchID, err, result.Replayed, len(repository.writes))
	}
}

func TestProductCreatorReplayRejectsDifferentSemanticRequest(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	repository.receipt = &model.ProductReceipt{Digest: "other"}
	result, err := creator.Create(t.Context(), command)
	if !errors.Is(err, model.ErrIdempotencyKeyReused) || result.Created.LaunchID != "" || repository.loads != 0 {
		t.Fatalf("launch=%q error=%v loads=%d", result.Created.LaunchID, err, repository.loads)
	}
}

func TestProductCreatorPendingReplayRemainsOriginalAfterReady(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	repository.before.Source.VariantStatus = "BLOCKED"
	repository.current = cloneProductSnapshot(t, repository.before)
	pending, err := creator.Create(t.Context(), command)
	if err != nil || pending.Status != 202 || len(repository.writes) != 0 || len(repository.jobs.writes) != 1 || len(repository.pending) != 1 {
		t.Fatalf("status=%d error=%v launches=%d jobs=%d", pending.Status, err, len(repository.writes), len(repository.jobs.writes))
	}
	repository.receipt = &repository.receipts[0]
	repository.before.Source.VariantStatus = "READY"
	repository.current.Source.VariantStatus = "READY"
	replay, err := creator.Create(t.Context(), command)
	if err != nil || replay.Status != 202 || !replay.Replayed || !bytes.Equal(pending.Body, replay.Body) || len(repository.jobs.writes) != 1 || len(repository.writes) != 0 {
		t.Fatalf("status=%d error=%v replay=%v launches=%d", replay.Status, err, replay.Replayed, len(repository.writes))
	}
}

func TestProductCreatorReadyValidationReentersPreparation(t *testing.T) {
	creator, repository, _, command := productFixture(t)
	repository.before.Source.VariantStatus = "BLOCKED"
	repository.jobs.found = true
	repository.jobs.current = model.ValidationJob{ID: validationFixtureID, State: "SUCCEEDED", ExecutionNo: 1, Version: 2}
	repository.afterCommit = func() { repository.before = repository.current }
	result, err := creator.Create(t.Context(), command)
	if err != nil || result.Status != 201 || repository.transactions != 2 || repository.loads != 2 || len(repository.writes) != 1 || len(repository.receipts) != 1 || len(repository.jobs.writes) != 0 {
		t.Fatalf("status=%d error=%v transactions=%d loads=%d launches=%d receipts=%d", result.Status, err, repository.transactions, repository.loads, len(repository.writes), len(repository.receipts))
	}
}
