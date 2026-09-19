package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/bootstrap/config"
	"retrom/internal/capability/security/authn"
	model "retrom/internal/model/accounts"
)

type initializationMemory struct {
	state     model.InitializationState
	plan      model.BootstrapPlan
	writes    int
	lateError error
}

func (memory *initializationMemory) State(context.Context) (model.InitializationState, error) {
	return memory.state, nil
}

func (memory *initializationMemory) Credentials(context.Context) ([]model.StoredCredential, error) {
	return nil, nil
}

func (memory *initializationMemory) WithWrite(_ context.Context, work func(model.InitializationScope) error) error {
	if err := work(model.InitializationScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *initializationMemory) Bootstrap(_ context.Context, plan model.BootstrapPlan) error {
	memory.plan = plan
	memory.writes++
	return nil
}

type setupProof struct{}

func (setupProof) SetupCode() string                  { return "setup-proof" }
func (setupProof) MatchesSetupCode(value string) bool { return value == "setup-proof" }

type initializationHasher struct{ duringHash func() }

func (initializationHasher) Verify(context.Context, string, string) (bool, error) { return true, nil }

func (hasher initializationHasher) Hash(context.Context, string) (string, error) {
	if hasher.duringHash != nil {
		hasher.duringHash()
	}
	return "initial-hash", nil
}

func initializationMint() (model.SessionMaterial, error) {
	return model.SessionMaterial{ID: "session"}, nil
}

func TestInitializationRejectsPartialPendingAndOrphanedCompletedState(t *testing.T) {
	for _, state := range []model.InitializationState{{State: "PENDING", Users: 1}, {State: "PENDING", Profiles: 1}, {State: "COMPLETED"}, {State: "COMPLETED", EnabledAdmins: 1, OrphanProfiles: 1}, {State: "UNKNOWN"}} {
		memory := &initializationMemory{state: state}
		err := NewInitialization(memory, InitializationOptions{Mode: config.ModeRelease}).Start(t.Context())
		if !errors.Is(err, model.ErrInitializationState) || memory.writes != 0 {
			t.Fatalf("invalid initialization %+v: %v", state, err)
		}
	}
}

func TestSetupProofReadNeverWritesInstanceState(t *testing.T) {
	memory := &initializationMemory{state: model.InitializationState{State: "PENDING"}}
	service := NewInitialization(memory, InitializationOptions{Credentials: setupProof{}})
	proof, err := service.ReadSetupCode(t.Context())
	if err != nil || proof != "setup-proof" || memory.writes != 0 {
		t.Fatalf("setup proof: %q / %v", proof, err)
	}
	memory.state.Users = 1
	if _, err := service.ReadSetupCode(t.Context()); !errors.Is(err, model.ErrInitializationDone) {
		t.Fatalf("exposed setup proof for partial instance: %v", err)
	}
}

func TestInitializationRechecksStateAfterHashing(t *testing.T) {
	memory := &initializationMemory{state: model.InitializationState{State: "PENDING"}}
	options := InitializationOptions{Mode: config.ModeTest, Hasher: initializationHasher{duringHash: func() { memory.state.State = "COMPLETED" }}, Mint: initializationMint, Now: time.Now}
	err := NewInitialization(memory, options).Start(t.Context())
	if !errors.Is(err, model.ErrInitializationDone) || memory.writes != 0 {
		t.Fatalf("initialized concurrent instance: %v", err)
	}
}

func TestInitializationCommitFailureReturnsNoSession(t *testing.T) {
	memory := &initializationMemory{state: model.InitializationState{State: "PENDING"}, lateError: context.Canceled}
	options := InitializationOptions{Mode: config.ModeRelease, Credentials: setupProof{}, Hasher: initializationHasher{}, Blocklist: authn.EmptyBlocklist{}, Mint: initializationMint, Now: func() time.Time { return time.UnixMilli(100) }}
	session, err := NewInitialization(memory, options).Initialize(t.Context(), model.InitializeRequest{SetupCode: "setup-proof", Username: "admin", DisplayName: "Owner", Password: "initial passphrase", PasswordConfirmation: "initial passphrase"})
	if !errors.Is(err, context.Canceled) || session.Principal.SessionID != "" || memory.writes != 1 || memory.plan.Kind != "RELEASE_SETUP" || memory.plan.ActorLabel != "release-setup" {
		t.Fatalf("initialization commit: %+v / %v", memory.plan, err)
	}
}
