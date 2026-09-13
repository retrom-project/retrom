package netplay

import (
	"context"
	"fmt"

	"retrom/internal/transport/netplay/capability"

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
type ParticipantAccess struct {
	repository ParticipantAccessRepository
	signer     CredentialSigner
}

func NewParticipantAccess(repository ParticipantAccessRepository, signer CredentialSigner) *ParticipantAccess {
	return &ParticipantAccess{repository, signer}
}

func (service *ParticipantAccess) Authenticate(
	ctx context.Context,
	roomID, profileID, encoded string,
) (SocketParticipant, error) {
	record, err := service.repository.Socket(ctx, roomID, profileID)
	if err != nil {
		return SocketParticipant{}, fmt.Errorf("netplay/read socket access: %w", err)
	}
	if record.LaunchState != "ACTIVE" || !capability.MatchesCapability(encoded, record.CredentialHash) {
		return SocketParticipant{}, ErrForbidden
	}
	return record.Participant, nil
}

func (service *ParticipantAccess) Capability(ctx context.Context, sessionID, profileID string) (string, error) {
	record, err := service.repository.Credential(ctx, sessionID, profileID)
	if err != nil {
		return "", fmt.Errorf("netplay/read participant credential: %w", err)
	}
	credential, err := IssueParticipantCredential(service.signer, sessionID, profileID, record.Generation)
	if err != nil {
		return "", err
	}
	encoded := capability.EncodeCapability(credential)
	if !capability.MatchesCapability(encoded, record.CredentialHash) {
		return "", ErrForbidden
	}
	return encoded, nil
}

func IssueParticipantCredential(
	signer CredentialSigner,
	sessionID, profileID string,
	generation int64,
) ([32]byte, error) {
	if generation < 1 || generation > 1<<32-1 {
		return [32]byte{}, ErrForbidden
	}
	session, err := uuid.Parse(sessionID)
	if err != nil {
		return [32]byte{}, ErrForbidden
	}
	profile, err := uuid.Parse(profileID)
	if err != nil {
		return [32]byte{}, ErrForbidden
	}
	return signer.Capability(session, profile, uint32(generation)), nil
}
