package payloadrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	model "retrom/internal/model/payloadrelease"
	"testing"
)

type forbiddenEffects struct{ calls int }

func (repository *forbiddenEffects) WithEffects(context.Context, func(model.EffectScope) error) error {
	repository.calls++
	return errors.New("effect transaction entered")
}
func (*forbiddenEffects) ActiveMutations(context.Context, model.Scope) (int64, error) { return 0, nil }

func TestReleaseEffectsRejectsUnfrozenInputsBeforeTransaction(t *testing.T) {
	input := model.Input{
		SchemaVersion: 1,
		Kind:          "PAYLOAD_RELEASE",
		Scope:   model.Scope{Type: model.ScopeImportItem, ID: "item"},
		ExecutionID:   "c9fcb44e-c97f-4d7d-a141-713f0a4384c7",
		Inputs:        model.ScopeInputs{ScopeVersion: 7, Reason: model.ReasonImportDiscarded},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	work := model.Work{
		Kind:        input.Kind,
		Scope: input.Scope,
		InputFound:  true,
		InputJSON:   string(encoded),
		InputDigest: hex.EncodeToString(digest[:]),
	}
	input.Inputs.ScopeVersion++
	repository := &forbiddenEffects{}
	service := NewReleaseEffects(repository, nil, nil, nil, nil)
	err = service.Execute(t.Context(), model.Execution{Work: work, Input: input})
	if !errors.Is(err, model.ErrInputInvalid) || repository.calls != 0 {
		t.Fatalf("input error=%v transaction calls=%d", err, repository.calls)
	}
}
