package netplay

import (
	"context"
	"errors"
	"testing"
	"time"

	launchmodel "retrom/internal/model/launch"
	model "retrom/internal/model/netplay"
)

type preparationMemory struct {
	before, current                                          model.PreparationSnapshot
	reads, writes                                            int
	plan                                                     model.PreparationPlan
	readFailure, currentFailure, writeFailure, commitFailure error
}

func (memory *preparationMemory) Snapshot(context.Context, string, string, string) (model.PreparationSnapshot, error) {
	memory.reads++
	if memory.reads == 1 {
		return memory.before, memory.readFailure
	}
	return memory.current, memory.currentFailure
}

func (memory *preparationMemory) WithPreparation(_ context.Context, work func(model.PreparationScope) error) error {
	if err := work(model.PreparationScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.commitFailure
}

func (memory *preparationMemory) Record(_ context.Context, plan model.PreparationPlan) error {
	memory.plan = plan
	memory.writes++
	return memory.writeFailure
}

type preparationLauncher struct {
	request launchmodel.NetplayCreateRequest
	calls   int
	failure error
}

func (launcher *preparationLauncher) CreateNetplay(_ context.Context, request launchmodel.NetplayCreateRequest) (launchmodel.Created, error) {
	launcher.request = request
	launcher.calls++
	return launchmodel.Created{LaunchID: "launch", PlayURL: "/play/launch"}, launcher.failure
}

type preparationAborter struct {
	roomID, sessionID string
	calls             int
	cancelled         bool
	bounded           bool
	failure           error
}

func (aborter *preparationAborter) AbortPreparation(ctx context.Context, roomID, sessionID string) error {
	aborter.roomID = roomID
	aborter.sessionID = sessionID
	aborter.calls++
	aborter.cancelled = ctx.Err() != nil
	_, aborter.bounded = ctx.Deadline()
	return aborter.failure
}

func preparationFixture() (*ParticipantPreparation, *preparationMemory, *preparationLauncher, *preparationAborter, model.PreparationRequest) {
	request := model.PreparationRequest{RoomID: "room", SessionID: "01980000-0000-7000-8000-000000000001", ProfileID: "01980000-0000-7000-8000-000000000002", Capabilities: launchmodel.Capabilities{SecureContext: true, SharedArrayBuffer: true, CrossOriginIsolated: true}}
	before := model.PreparationSnapshot{Control: model.SessionControlSnapshot{RoomID: request.RoomID, SessionID: request.SessionID, State: "PREPARING", Version: 3, RoomVersion: 5}, Peer: model.SessionPeer{ProfileID: request.ProfileID, State: "LOCKED", PlayerNo: 2}, GameID: "game", VariantID: "variant", ProviderID: "provider", TargetID: "target", BundleSHA256: "bundle", Locked: 1}
	current := before
	current.Peer.State = "LAUNCH_READY"
	current.Peer.CredentialGeneration = 1
	current.Locked = 0
	memory := &preparationMemory{before: before, current: current}
	launcher := &preparationLauncher{}
	aborter := &preparationAborter{}
	signer := &participantSigner{value: [32]byte{1}}
	return NewParticipantPreparation(memory, signer, aborter, func() time.Time { return time.UnixMilli(1786000000000) }), memory, launcher, aborter, request
}

func TestParticipantPreparationFreezesLaunchAndPublishesAfterCommit(t *testing.T) {
	t.Parallel()
	service, memory, launcher, aborter, request := preparationFixture()
	result, err := service.Launch(t.Context(), launcher, request)
	if err != nil || result.Launch.LaunchID != "launch" || result.RoomCapability == "" || result.CredentialExpiry != 1786028800000 {
		t.Fatalf("launch id=%s issued=%v expiry=%d error=%v", result.Launch.LaunchID, result.RoomCapability != "", result.CredentialExpiry, err)
	}
	assertFrozenPreparationLaunch(t, launcher.request, request)
	if memory.writes != 1 || !memory.plan.AdvanceLoading || len(memory.plan.Events) != 2 || aborter.calls != 0 {
		t.Fatalf("writes=%d plan=%+v aborts=%d", memory.writes, memory.plan, aborter.calls)
	}
}

func TestParticipantPreparationReissueDoesNotDuplicateTransition(t *testing.T) {
	t.Parallel()
	service, memory, launcher, _, request := preparationFixture()
	memory.before = memory.current
	memory.before.Control.State = "LOADING"
	memory.before.LaunchRecorded = true
	memory.current = memory.before
	if _, err := service.Launch(t.Context(), launcher, request); err != nil {
		t.Fatal(err)
	}
	if memory.writes != 0 || launcher.request.CredentialGeneration != 1 {
		t.Fatalf("reissue writes=%d generation=%d", memory.writes, launcher.request.CredentialGeneration)
	}
}

func TestParticipantPreparationFailuresDoNotPublishCredentials(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("preparation failed")
	for _, phase := range []string{"read", "launcher", "record read", "record write", "commit"} {
		t.Run(phase, func(t *testing.T) {
			service, memory, launcher, aborter, request := preparationFixture()
			switch phase {
			case "read":
				memory.readFailure = sentinel
			case "launcher":
				launcher.failure = sentinel
			case "record read":
				memory.currentFailure = sentinel
			case "record write":
				memory.writeFailure = sentinel
			case "commit":
				memory.commitFailure = sentinel
			}
			result, err := service.Launch(t.Context(), launcher, request)
			if !errors.Is(err, sentinel) || result.Launch.LaunchID != "" || result.RoomCapability != "" {
				t.Fatalf("failure leaked launch=%v credential=%v error=%v", result.Launch.LaunchID != "", result.RoomCapability != "", err)
			}
			expected := 1
			if phase == "read" {
				expected = 0
			}
			if aborter.calls != expected {
				t.Fatalf("abort count=%d", aborter.calls)
			}
			assertPreparationAbortScope(t, aborter, expected, request.SessionID)
		})
	}
}

func TestParticipantPreparationCleanupSurvivesCancellationAndPreservesBothErrors(t *testing.T) {
	t.Parallel()
	service, _, _, aborter, request := preparationFixture()
	original, cleanup := errors.New("launch failed"), errors.New("cleanup failed")
	aborter.failure = cleanup
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := service.Fail(ctx, request.RoomID, request.SessionID, original)
	if !errors.Is(err, original) || !errors.Is(err, cleanup) || aborter.cancelled || !aborter.bounded {
		t.Fatalf("cleanup scope=%+v error=%v", aborter, err)
	}
}

func TestParticipantPreparationRejectsReplacementCredential(t *testing.T) {
	t.Parallel()
	service, memory, launcher, aborter, request := preparationFixture()
	memory.current.Peer.CredentialGeneration = 2
	result, err := service.Launch(t.Context(), launcher, request)
	if !errors.Is(err, model.ErrRoomConflict) || result.RoomCapability != "" || memory.writes != 0 || aborter.calls != 1 {
		t.Fatalf("replacement writes=%d aborts=%d error=%v", memory.writes, aborter.calls, err)
	}
}

func assertFrozenPreparationLaunch(t *testing.T, sent launchmodel.NetplayCreateRequest, request model.PreparationRequest) {
	t.Helper()
	if sent.GameID != "game" || sent.GameVariantID != "variant" || sent.PlayerNo != 2 || sent.CredentialGeneration != 1 || sent.ReturnTo != "/netplay/rooms/room" || len(sent.NetplayCredentialSHA256) != 32 || sent.ClientCapabilities != request.Capabilities {
		t.Fatalf("invalid frozen launch game=%s variant=%s generation=%d", sent.GameID, sent.GameVariantID, sent.CredentialGeneration)
	}
}

func assertPreparationAbortScope(t *testing.T, aborter *preparationAborter, expected int, sessionID string) {
	t.Helper()
	if expected == 1 && (aborter.sessionID != sessionID || aborter.cancelled || !aborter.bounded) {
		t.Fatalf("abort scope=%+v", aborter)
	}
}
