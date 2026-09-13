package launch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"retrom/internal/capability/runtime/runtimebundle"
)

type netplayCreationProvider struct {
	target runtimebundle.Target
	digest string
	before func()
}

func (provider netplayCreationProvider) Target(string, string) (runtimebundle.Target, bool) {
	if provider.before != nil {
		provider.before()
	}
	return provider.target, true
}

func (provider netplayCreationProvider) BundleSHA256(string, string) (string, bool) {
	return provider.digest, true
}

func cloneNetplaySnapshot(t *testing.T, snapshot NetplayCreationSnapshot) NetplayCreationSnapshot {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var result NetplayCreationSnapshot
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func netplayCreatorFixture(t *testing.T) (*NetplayCreator, *netplayCreationMemory, NetplayCreateRequest) {
	t.Helper()
	request := netplayTestRequest()
	authority := NetplayCreationAuthority{
		SessionID: request.SessionID, RoomID: request.RoomID, GameID: request.GameID, VariantID: request.GameVariantID, CoreID: "fceumm",
		ProviderID: request.ProviderID, TargetID: request.TargetID, BundleDigest: request.BundleSHA256,
		SessionState: "PREPARING", RoomState: "STARTING", CurrentSessionID: request.SessionID,
		ProfileID: request.ProfileID, MemberID: "member", ParticipantState: "LOCKED", PlayerNo: 1, ParticipantVersion: 1,
	}
	snapshot := NetplayCreationSnapshot{Found: true, Authority: authority, Product: ProductSnapshot{
		Found: true,
		Source: ProductSource{
			GameID:     request.GameID,
			VariantID:  request.GameVariantID,
			CoreID:     "fceumm",
			ProviderID: request.ProviderID,

			TargetID:        request.TargetID,
			BundleSHA256:    request.BundleSHA256,
			ContentKind:     "SINGLE_FILE",
			DeliveryProfile: "EMULATORJS_CONTENT",

			VariantStatus:      "READY",
			DependencySnapshot: `{"schemaVersion":1,"kind":"STATIC","bios":[]}`,
			GameVersion:        1,
		}, GameFiles: []ProductFile{
			{Role: "CONTENT", LogicalName: "game.nes", BlobID: "blob", Digest: strings.Repeat("b", 64), SizeBytes: 16},
		},
	}}
	repository := &netplayCreationMemory{before: snapshot, current: cloneNetplaySnapshot(t, snapshot)}
	provider := netplayCreationProvider{digest: request.BundleSHA256, before: func() {
		if repository.inTransaction {
			t.Fatal("provider entered writer transaction")
		}
	}}
	provider.target.Capabilities.NetplayPort = true
	environment := NetplayCreationEnvironment{
		Now: func() time.Time { return time.UnixMilli(1000) }, NewID: func() (string, error) { return previewTestID, nil },
		SignCapability: func(string) (string, []byte, error) { return "fixture-capability", make([]byte, 32), nil },
	}
	return NewNetplayCreator(repository, provider, nil, environment), repository, request
}

func existingNetplaySnapshot(snapshot *NetplayCreationSnapshot, request NetplayCreateRequest) {
	sessionID, player := request.SessionID, int64(request.PlayerNo)
	snapshot.Existing = &NetplayExistingLaunch{
		ID: previewTestID, ProfileID: request.ProfileID, GameID: request.GameID,
		ProviderID: request.ProviderID, TargetID: request.TargetID, BundleDigest: request.BundleSHA256, State: "CREATED",
		SessionID: &sessionID, PlayerNo: &player, CredentialHash: make([]byte, 32), BootstrapEnd: 301000, HardEnd: 28801000,
	}
	snapshot.Authority.ParticipantState = "LAUNCH_READY"
	snapshot.Authority.Generation = 1
	snapshot.Authority.CredentialHash = make([]byte, 32)
}
