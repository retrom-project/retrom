package launch

import (
	"context"
	"time"
)

type NetplayCreationAuthority struct {
	SessionID, RoomID, GameID, VariantID, CoreID, ProviderID, TargetID, BundleDigest string
	SessionState, RoomState, CurrentSessionID, ProfileID, MemberID, ParticipantState string
	PlayerNo, Generation, ParticipantVersion                                         int64
	CredentialHash                                                                   []byte
}

type NetplayExistingLaunch struct {
	ID, ProfileID, GameID, ProviderID, TargetID, BundleDigest, State string
	SessionID                                                        *string
	PlayerNo                                                         *int64
	CredentialHash                                                   []byte
	BootstrapEnd, HardEnd                                            int64
}

type NetplayCreationSnapshot struct {
	Found     bool
	Authority NetplayCreationAuthority
	Product   ProductSnapshot
	Existing  *NetplayExistingLaunch
}

type NetplayCreationPlan struct {
	Request                      NetplayCreateRequest
	Before                       NetplayCreationAuthority
	Source                       ProductSource
	ID                           string
	CredentialHash               []byte
	Content                      ProductContent
	External                     []ProductExternalFile
	NowMS, BootstrapEnd, HardEnd int64
}

type NetplayCreationRepository interface {
	LoadNetplaySnapshot(context.Context, NetplayCreateRequest) (NetplayCreationSnapshot, error)
	CommitNetplayCreation(context.Context, NetplayCreationPlan) error
}

type NetplayCreationEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
}
