package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/content/contentcapability"
)

type validationTestRepository struct {
	mutex                               sync.Mutex
	work                                model.ValidationWork
	facts                               model.ValidationFacts
	terminal                            model.ValidationTerminal
	events                              []string
	variants                            int
	readFacts                           func(context.Context) (model.ValidationFacts, error)
	factsCallCount                      int
	finishError, claimError, renewError error
	renewals                            int
}

func (repository *validationTestRepository) LoadValidationWork(_ context.Context, _ string) (model.ValidationWork, bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	return repository.work, true, nil
}

func (repository *validationTestRepository) LoadValidationFacts(ctx context.Context, _ model.ValidationInputs) (model.ValidationFacts, error) {
	repository.mutex.Lock()
	repository.factsCallCount++
	call := repository.factsCallCount
	repository.mutex.Unlock()
	if call == 1 && repository.readFacts != nil {
		return repository.readFacts(ctx)
	}
	return repository.facts, nil
}

func (repository *validationTestRepository) LoadValidationCandidates(context.Context, int64) ([]string, error) {
	return []string{repository.work.ID}, nil
}

func (repository *validationTestRepository) CommitValidationClaim(_ context.Context, plan model.ValidationClaimWrite) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.claimError != nil {
		return repository.claimError
	}
	repository.work.State = "RUNNING"
	repository.work.WorkerID = plan.WorkerID
	repository.work.Version++
	repository.work.Attempt++
	repository.work.StartedMS, repository.work.DeadlineMS, repository.work.LeaseMS = &plan.StartedMS, &plan.DeadlineMS, &plan.LeaseMS
	repository.events = append(repository.events, "STARTED")
	return nil
}

func (repository *validationTestRepository) CommitValidationRenewal(_ context.Context, _ model.ValidationClaim, _ int64, lease int64) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.renewError != nil {
		return repository.renewError
	}
	repository.renewals++
	repository.work.Version++
	repository.work.LeaseMS = &lease
	return nil
}

func (repository *validationTestRepository) CommitValidationFinish(_ context.Context, plan model.ValidationTerminal) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.finishError != nil {
		return repository.finishError
	}
	repository.terminal = plan
	repository.work.State = plan.State
	repository.work.Version++
	repository.events = append(repository.events, plan.State)
	return nil
}

func (repository *validationTestRepository) CommitValidationRecovery(_ context.Context, plan model.ValidationRecovery) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	repository.work.Version++
	if plan.Terminal {
		repository.work.State = "FAILED"
		repository.events = append(repository.events, "FAILED")
		return nil
	}
	repository.work.State = "QUEUED"
	repository.work.WorkerID = ""
	repository.work.LeaseMS = nil
	return nil
}

func (repository *validationTestRepository) CommitValidationSettlement(_ context.Context, settlement model.ValidationSettlement) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.finishError != nil {
		return repository.finishError
	}
	repository.variants++
	repository.terminal = settlement.Finish
	repository.work.State = settlement.Finish.State
	repository.work.Version++
	repository.events = append(repository.events, settlement.Finish.State)
	return nil
}

type validationTestTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
}

func (ticker *validationTestTicker) Ticks() <-chan time.Time { return ticker.ticks }
func (ticker *validationTestTicker) Stop()                   { close(ticker.stopped) }

func newValidationTestWorker(t *testing.T) (*ValidationWorker, *validationTestRepository, *validationTestTicker) {
	t.Helper()
	source := model.ProductSource{
		GameID: "game", VariantID: "variant", GameVersion: 1, SourceManifestDigest: "manifest", CoreID: "gambatte",
		ProviderID: "retrom-runtime", TargetID: "gambatte", ContentKind: "SINGLE_FILE", PlatformID: "gbc", ValidationLogicalName: "game.gb",
		ContentPolicy: contentcapability.NewPolicy("SINGLE_FILE"),
	}
	facts := model.ValidationFacts{Found: true, BindingFound: true, RelationshipEnabled: true, Content: model.ProductSnapshot{Found: true, Source: source}}
	inputs, err := ProductValidationInputs(facts.Content, source.VariantID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.ValidationSnapshot{SchemaVersion: 1, Kind: "VARIANT_VALIDATE", Scope: model.ValidationScope{Type: "GAME_VARIANT", ID: "variant"}, ExecutionID: "01980000-0000-7000-8000-000000000002", Inputs: inputs}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	repository := &validationTestRepository{facts: facts, work: model.ValidationWork{ID: "job", Kind: "VARIANT_VALIDATE", ScopeType: "GAME_VARIANT", ScopeID: "variant", State: "QUEUED", Version: 1, ExecutionNo: 1, MaxAttempts: 2, SnapshotJSON: string(encoded), InputDigest: hex.EncodeToString(digest[:])}}
	ticker := &validationTestTicker{ticks: make(chan time.Time), stopped: make(chan struct{})}
	worker := NewValidationWorker(repository, model.ValidationWorkerEnvironment{Now: func() time.Time { return time.UnixMilli(1_000_000) }, NewID: func() (string, error) { return "01980000-0000-7000-8000-000000000003", nil }, NewTicker: func(time.Duration) model.ValidationTicker { return ticker }})
	return worker, repository, ticker
}

func validationTestCause(t *testing.T, err, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("want cause %v, got %v", cause, err)
	}
}
