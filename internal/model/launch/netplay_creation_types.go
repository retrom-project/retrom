package launch

import (
	"context"
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

type NetplayCreationScope interface {
	Snapshot(context.Context, NetplayCreateRequest) (NetplayCreationSnapshot, error)
	Create(context.Context, NetplayCreationPlan) error
}

type NetplayCreationRepository interface {
	Snapshot(context.Context, NetplayCreateRequest) (NetplayCreationSnapshot, error)
	WithCreation(context.Context, func(NetplayCreationScope) error) error
}
