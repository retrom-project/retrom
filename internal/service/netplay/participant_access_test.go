package netplay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"

	model "retrom/internal/model/netplay"

	"github.com/google/uuid"
)

type participantAccessMemory struct {
	socket     model.SocketAccessRecord
	credential model.ParticipantCredentialRecord
	failure    error
}

func (memory *participantAccessMemory) Socket(context.Context, string, string) (model.SocketAccessRecord, error) {
	return memory.socket, memory.failure
}

func (memory *participantAccessMemory) Credential(context.Context, string, string) (model.ParticipantCredentialRecord, error) {
	return memory.credential, memory.failure
}

type participantSigner struct {
	generation           uint32
	sessionID, profileID uuid.UUID
	value                [32]byte
}

func (signer *participantSigner) Capability(sessionID, profileID uuid.UUID, generation uint32) [32]byte {
	signer.sessionID = sessionID
	signer.profileID = profileID
	signer.generation = generation
	return signer.value
}

func accessFixture() (*ParticipantAccess, *participantAccessMemory, *participantSigner, string) {
	signer := &participantSigner{value: [32]byte{1, 2, 3}}
	encoded := base64.RawURLEncoding.EncodeToString(signer.value[:])
	hash := sha256.Sum256(signer.value[:])
	memory := &participantAccessMemory{socket: model.SocketAccessRecord{Participant: model.SocketParticipant{RoomID: "room", SessionID: "session", ProfileID: "profile", PlayerNo: 2, CredentialGeneration: 3}, LaunchState: "ACTIVE", CredentialHash: hash[:]}, credential: model.ParticipantCredentialRecord{Generation: 3, CredentialHash: hash[:]}}
	return NewParticipantAccess(memory, signer), memory, signer, encoded
}

func TestParticipantAccessAuthenticatesOnlyActiveMatchingCapability(t *testing.T) {
	t.Parallel()
	service, memory, _, encoded := accessFixture()
	peer, err := service.Authenticate(t.Context(), "room", "profile", encoded)
	if err != nil || peer.PlayerNo != 2 || peer.CredentialGeneration != 3 {
		t.Fatalf("peer=%+v error=%v", peer, err)
	}
	for _, state := range []string{"CREATED", "REVOKED", "EXPIRED"} {
		memory.socket.LaunchState = state
		if peer, err := service.Authenticate(t.Context(), "room", "profile", encoded); !errors.Is(err, model.ErrForbidden) || peer.RoomID != "" {
			t.Fatalf("inactive peer=%+v error=%v", peer, err)
		}
	}
	memory.socket.LaunchState = "ACTIVE"
	for _, invalid := range []string{"invalid", encoded + "=", base64.RawURLEncoding.EncodeToString(make([]byte, 32))} {
		if _, err := service.Authenticate(t.Context(), "room", "profile", invalid); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("invalid token error=%v", err)
		}
	}
}

func TestParticipantAccessCredentialPreservesGenerationAndIdentity(t *testing.T) {
	t.Parallel()
	service, _, signer, encoded := accessFixture()
	sessionID, profileID := "01980000-0000-7000-8000-000000000001", "01980000-0000-7000-8000-000000000002"
	value, err := service.Capability(t.Context(), sessionID, profileID)
	if err != nil || value != encoded || signer.generation != 3 || signer.sessionID.String() != sessionID || signer.profileID.String() != profileID {
		t.Fatalf("issued matching=%v generation=%d error=%v", value == encoded, signer.generation, err)
	}
}

func TestParticipantAccessRejectsMalformedOrOverflowedGeneration(t *testing.T) {
	t.Parallel()
	for _, generation := range []int64{0, -1, 1 << 32, 1<<32 + 1} {
		t.Run("generation", func(t *testing.T) {
			service, memory, signer, _ := accessFixture()
			memory.credential.Generation = generation
			_, err := service.Capability(t.Context(), "01980000-0000-7000-8000-000000000001", "01980000-0000-7000-8000-000000000002")
			if !errors.Is(err, model.ErrForbidden) || signer.generation != 0 {
				t.Fatalf("generation=%d error=%v", generation, err)
			}
		})
	}
	service, _, signer, _ := accessFixture()
	if _, err := service.Capability(t.Context(), "invalid", "invalid"); !errors.Is(err, model.ErrForbidden) || signer.generation != 0 {
		t.Fatalf("invalid identity=%v", err)
	}
}

func TestParticipantAccessPreservesStorageFailuresAndReturnsNoCredentials(t *testing.T) {
	t.Parallel()
	service, memory, _, encoded := accessFixture()
	sentinel := errors.New("database unavailable")
	memory.failure = sentinel
	if peer, err := service.Authenticate(t.Context(), "room", "profile", encoded); !errors.Is(err, sentinel) || peer.RoomID != "" {
		t.Fatalf("failed peer=%+v error=%v", peer, err)
	}
	if credential, err := service.Capability(t.Context(), "session", "profile"); !errors.Is(err, sentinel) || credential != "" {
		t.Fatalf("credential leaked=%v error=%v", credential != "", err)
	}
	memory.failure = nil
	memory.credential.CredentialHash = make([]byte, 32)
	if credential, err := service.Capability(t.Context(), "01980000-0000-7000-8000-000000000001", "01980000-0000-7000-8000-000000000002"); !errors.Is(err, model.ErrForbidden) || credential != "" {
		t.Fatalf("mismatched credential leaked=%v error=%v", credential != "", err)
	}
}
