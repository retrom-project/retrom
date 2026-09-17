package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	model "retrom/internal/model/launch"
	"sync"
	"testing"
	"time"

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
	finishError, claimError, renewError error
	renewals                            int
}

type validationTestScope struct{ repository *validationTestRepository }

func (repository *validationTestRepository) WithWorker(ctx context.Context, operation func(model.ValidationWorkerScope) error) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	work, terminal, variants, count := repository.work, repository.terminal, repository.variants, len(repository.events)
	scope := validationTestScope{repository}
	if err := operation(model.ValidationWorkerScope{Jobs: scope, Facts: scope, Variants: scope}); err != nil {
		repository.work, repository.terminal, repository.variants = work, terminal, variants
		repository.events = repository.events[:count]
		return err
	}
	return nil
}

func (repository *validationTestRepository) Facts(ctx context.Context, _ model.ValidationInputs) (model.ValidationFacts, error) {
	if repository.readFacts != nil {
		return repository.readFacts(ctx)
	}
	return repository.facts, nil
}

func (repository *validationTestRepository) Candidates(context.Context, int64) ([]string, error) {
	return []string{repository.work.ID}, nil
}

func (scope validationTestScope) Read(context.Context, string) (model.ValidationWork, bool, error) {
	return scope.repository.work, true, nil
}

func (scope validationTestScope) Facts(context.Context, model.ValidationInputs) (model.ValidationFacts, error) {
	return scope.repository.facts, nil
}

func (scope validationTestScope) Claim(_ context.Context, plan model.ValidationClaimWrite) error {
	repository := scope.repository
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

func (scope validationTestScope) Renew(_ context.Context, _ model.ValidationClaim, _ int64, lease int64) error {
	repository := scope.repository
	if repository.renewError != nil {
		return repository.renewError
	}
	repository.renewals++
	repository.work.Version++
	repository.work.LeaseMS = &lease
	return nil
}

func (scope validationTestScope) Finish(_ context.Context, plan model.ValidationTerminal) error {
	repository := scope.repository
	repository.terminal = plan
	repository.work.State = plan.State
	repository.work.Version++
	if repository.finishError != nil {
		return repository.finishError
	}
	repository.events = append(repository.events, plan.State)
	return nil
}

func (scope validationTestScope) Recover(_ context.Context, plan model.ValidationRecovery) error {
	repository := scope.repository
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

func (scope validationTestScope) Apply(context.Context, model.ValidationVariantWrite) error {
	scope.repository.variants++
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
