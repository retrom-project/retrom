package netplay

import (
	"context"
	"fmt"

	model "retrom/internal/model/netplay"
	"retrom/internal/transport/netplay/capability"

	"github.com/google/uuid"
)

type ParticipantAccess struct {
	repository model.ParticipantAccessRepository
	signer     model.CredentialSigner
}

func NewParticipantAccess(
	repository model.ParticipantAccessRepository,
	signer model.CredentialSigner,
) *ParticipantAccess {
	return &ParticipantAccess{repository, signer}
}

func (service *ParticipantAccess) Authenticate(
	ctx context.Context,
	roomID, profileID, encoded string,
) (model.SocketParticipant, error) {
	record, err := service.repository.Socket(ctx, roomID, profileID)
	if err != nil {
		return model.SocketParticipant{}, fmt.Errorf("netplay/read socket access: %w", err)
	}
	if record.LaunchState != "ACTIVE" || !capability.MatchesCapability(encoded, record.CredentialHash) {
		return model.SocketParticipant{}, model.ErrForbidden
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
		return "", model.ErrForbidden
	}
	return encoded, nil
}

func IssueParticipantCredential(
	signer model.CredentialSigner,
	sessionID, profileID string,
	generation int64,
) ([32]byte, error) {
	if generation < 1 || generation > 1<<32-1 {
		return [32]byte{}, model.ErrForbidden
	}
	session, err := uuid.Parse(sessionID)
	if err != nil {
		return [32]byte{}, model.ErrForbidden
	}
	profile, err := uuid.Parse(profileID)
	if err != nil {
		return [32]byte{}, model.ErrForbidden
	}
	return signer.Capability(session, profile, uint32(generation)), nil
}
