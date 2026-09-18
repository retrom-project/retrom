package netplay

import (
	"context"

	"retrom/internal/model/launch"

	"github.com/google/uuid"
)

type SocketParticipant struct {
	RoomID, SessionID, ProfileID                      string
	PlayerNo                                          int
	CredentialGeneration                              int64
	ProfileDigest, ProviderID, TargetID, BundleSHA256 string
	RoomVersion, SessionVersion                       int64
	SessionState                                      string
	OccupiedSeatMask, PlayerCount                     int
}

type SocketAccessRecord struct {
	Participant    SocketParticipant
	CredentialHash []byte
	LaunchState    string
}

type ParticipantCredentialRecord struct {
	Generation     int64
	CredentialHash []byte
}

type ParticipantAccessRepository interface {
	Socket(context.Context, string, string) (SocketAccessRecord, error)
	Credential(context.Context, string, string) (ParticipantCredentialRecord, error)
}

type CredentialSigner interface {
	Capability(uuid.UUID, uuid.UUID, uint32) [32]byte
}

type PreparationRequest struct {
	RoomID, SessionID, ProfileID string
	Capabilities                 launch.Capabilities
}

type ParticipantLaunchResult struct {
	Launch           launch.Created
	RoomCapability   string
	CredentialExpiry int64
}

type PreparationSnapshot struct {
	Control                                               SessionControlSnapshot
	Peer                                                  SessionPeer
	GameID, VariantID, ProviderID, TargetID, BundleSHA256 string
	LaunchRecorded                                        bool
	Locked                                                int
}

type PreparationPlan struct {
	Before         PreparationSnapshot
	Events         []SessionEvent
	AdvanceLoading bool
	Now            int64
}

type PreparationRepository interface {
	Snapshot(context.Context, string, string, string) (PreparationSnapshot, error)
	CommitPreparation(context.Context, PreparationPlan) error
}

type NetplayLauncher interface {
	CreateNetplay(context.Context, launch.NetplayCreateRequest) (launch.Created, error)
}

type PreparationAborter interface {
	AbortPreparation(context.Context, string, string) error
}
