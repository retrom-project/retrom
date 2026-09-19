package launch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	model "retrom/internal/model/launch"
)

type netplayCreationMemory struct {
	before, current    model.NetplayCreationSnapshot
	cause, commitError error
	loadCount          int
	writes             []model.NetplayCreationPlan
}

func (repository *netplayCreationMemory) LoadNetplaySnapshot(
	_ context.Context,
	_ model.NetplayCreateRequest,
) (model.NetplayCreationSnapshot, error) {
	repository.loadCount++
	if repository.loadCount > 1 {
		return repository.current, repository.cause
	}
	return repository.before, repository.cause
}

func (repository *netplayCreationMemory) CommitNetplayCreation(_ context.Context, plan model.NetplayCreationPlan) error {
	repository.writes = append(repository.writes, plan)
	return repository.commitError
}

func netplayTestRequest() model.NetplayCreateRequest {
	return model.NetplayCreateRequest{
		SessionID: "session", RoomID: "room", GameID: "game", GameVariantID: "variant", ProfileID: "profile", PlayerNo: 1,
		ProviderID: "provider", TargetID: "target", BundleSHA256: strings.Repeat("a", 64), CredentialGeneration: 1,
		NetplayCredentialSHA256: make([]byte, 32), ReturnTo: "/netplay/rooms/room",
	}
}

func TestNetplayCreatorPreservesSnapshotCause(t *testing.T) {
	t.Parallel()
	repository := &netplayCreationMemory{cause: context.Canceled}
	service := NewNetplayCreator(
		repository,
		nil,
		nil,
		NetplayCreationEnvironment{Now: func() time.Time { return time.UnixMilli(1000) }},
	)
	result, err := service.CreateNetplay(t.Context(), netplayTestRequest())
	if !errors.Is(err, context.Canceled) || result.LaunchID != "" || len(repository.writes) != 0 {
		t.Fatalf("read cause: %v", err)
	}
}

func TestNetplayCreatorChecksIdentityBeforeWriting(t *testing.T) {
	t.Parallel()
	cause := errors.New("netplay entropy unavailable")
	service, repository, request := netplayCreatorFixture(t)
	service.environment.NewID = func() (string, error) { return "", cause }
	result, err := service.CreateNetplay(t.Context(), request)
	if !errors.Is(err, cause) || result.LaunchID != "" || len(repository.writes) != 0 {
		t.Fatalf("entropy failure=%v", err)
	}
	service.environment.NewID = func() (string, error) { return "not-a-uuid", nil }
	if _, err := service.CreateNetplay(t.Context(), request); !errors.Is(err, model.ErrBlocked) || len(repository.writes) != 0 {
		t.Fatalf("malformed ID=%v", err)
	}
}

func TestNetplayCreatorSuppressesSuccessWhenCommitFails(t *testing.T) {
	t.Parallel()
	service, repository, request := netplayCreatorFixture(t)
	cause := errors.New("netplay commit unavailable")
	repository.commitError = cause
	result, err := service.CreateNetplay(t.Context(), request)
	if !errors.Is(err, cause) || result.LaunchID != "" || result.Capability != "" || len(repository.writes) != 1 {
		t.Fatalf("commit failure returned result: %v", err)
	}
}

func TestNetplayCreatorReusesParticipantCreatedDuringPreparation(t *testing.T) {
	t.Parallel()
	service, repository, request := netplayCreatorFixture(t)
	existingNetplaySnapshot(&repository.current, request)
	result, err := service.CreateNetplay(t.Context(), request)
	if err != nil || !result.Existing || result.LaunchID != previewTestID || len(repository.writes) != 0 {
		t.Fatalf("concurrent replay=%q/%t error=%v", result.LaunchID, result.Existing, err)
	}
}

func TestNetplayCreatorAllowsUnrelatedSessionProgress(t *testing.T) {
	t.Parallel()
	service, repository, request := netplayCreatorFixture(t)
	repository.current.Authority.SessionState = "LOADING"
	repository.current.Product.Source.GameVersion++
	result, err := service.CreateNetplay(t.Context(), request)
	if err != nil || result.LaunchID == "" || len(repository.writes) != 1 {
		t.Fatalf("legitimate progress rejected: %v", err)
	}
}

func TestNetplayCreatorPreservesSigningCauseBeforeWriting(t *testing.T) {
	t.Parallel()
	service, repository, request := netplayCreatorFixture(t)
	cause := errors.New("netplay signing unavailable")
	service.environment.SignCapability = func(string) (string, []byte, error) { return "", nil, cause }
	result, err := service.CreateNetplay(t.Context(), request)
	if !errors.Is(err, cause) || result.LaunchID != "" || result.Capability != "" || len(repository.writes) != 0 {
		t.Fatalf("signing failure returned result: %v", err)
	}
}
