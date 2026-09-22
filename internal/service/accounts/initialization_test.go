package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/config"
)

type initializationMemory struct {
	state     InitializationState
	plan      BootstrapPlan
	writes    int
	lateError error
}

func (memory *initializationMemory) State(context.Context) (InitializationState, error) {
	return memory.state, nil
}

func (memory *initializationMemory) Credentials(context.Context) ([]StoredCredential, error) {
	return nil, nil
}

func (memory *initializationMemory) WithWrite(_ context.Context, work func(InitializationScope) error) error {
	if err := work(InitializationScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.lateError
}

func (memory *initializationMemory) Bootstrap(_ context.Context, plan BootstrapPlan) error {
	memory.plan = plan
	memory.writes++
	return nil
}

type initializationHasher struct{ duringHash func() }

func (initializationHasher) Verify(context.Context, string, string) (bool, error) { return true, nil }

func (hasher initializationHasher) Hash(context.Context, string) (string, error) {
	if hasher.duringHash != nil {
		hasher.duringHash()
	}
	return "initial-hash", nil
}
func initializationMint() (SessionMaterial, error) { return SessionMaterial{ID: "session"}, nil }
func TestInitializationRejectsPartialPendingAndOrphanedCompletedState(t *testing.T) {
	for _, state := range []InitializationState{{State: "PENDING", Users: 1}, {State: "PENDING", Profiles: 1}, {State: "COMPLETED"}, {State: "COMPLETED", EnabledAdmins: 1, OrphanProfiles: 1}, {State: "UNKNOWN"}} {
		memory := &initializationMemory{state: state}
		err := NewInitialization(memory, InitializationOptions{Mode: config.ModeRelease}).Start(t.Context())
		if !errors.Is(err, ErrInitializationState) || memory.writes != 0 {
			t.Fatalf("invalid initialization %+v: %v", state, err)
		}
	}
}

func TestInitializationRechecksStateAfterHashing(t *testing.T) {
	memory := &initializationMemory{state: InitializationState{State: "PENDING"}}
	options := InitializationOptions{Mode: config.ModeTest, Hasher: initializationHasher{duringHash: func() { memory.state.State = "COMPLETED" }}, Mint: initializationMint, Now: time.Now}
	err := NewInitialization(memory, options).Start(t.Context())
	if !errors.Is(err, ErrInitializationDone) || memory.writes != 0 {
		t.Fatalf("initialized concurrent instance: %v", err)
	}
}

func TestReleaseStartLeavesEmptyInstancePending(t *testing.T) {
	memory := &initializationMemory{state: InitializationState{State: "PENDING"}}
	service := NewInitialization(memory, InitializationOptions{Mode: config.ModeRelease})
	if err := service.Start(t.Context()); err != nil || memory.writes != 0 {
		t.Fatalf("release startup wrote an account: writes=%d error=%v", memory.writes, err)
	}
}

func TestTestModeRejectsManualInitialization(t *testing.T) {
	memory := &initializationMemory{state: InitializationState{State: "PENDING"}}
	service := NewInitialization(memory, InitializationOptions{Mode: config.ModeTest})
	if _, err := service.Initialize(t.Context(), InitializeRequest{}); !errors.Is(err, ErrInitializationDone) || memory.writes != 0 {
		t.Fatalf("test mode accepted manual initialization: writes=%d error=%v", memory.writes, err)
	}
}

func TestInitializationCommitFailureReturnsNoSession(t *testing.T) {
	memory := &initializationMemory{state: InitializationState{State: "PENDING"}, lateError: context.Canceled}
	options := InitializationOptions{Mode: config.ModeRelease, Hasher: initializationHasher{}, Blocklist: authn.EmptyBlocklist{}, Mint: initializationMint, Now: func() time.Time { return time.UnixMilli(100) }}
	session, err := NewInitialization(memory, options).Initialize(t.Context(), InitializeRequest{Username: "admin", DisplayName: "Owner", Password: "initial passphrase", PasswordConfirmation: "initial passphrase"})
	if !errors.Is(err, context.Canceled) || session.Principal.SessionID != "" || memory.writes != 1 || memory.plan.Kind != "RELEASE_SETUP" || memory.plan.ActorLabel != "release-setup" {
		t.Fatalf("initialization commit: %+v / %v", memory.plan, err)
	}
}
