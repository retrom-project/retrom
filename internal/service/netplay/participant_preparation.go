package netplay

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	launch "retrom/internal/service/launch"
	"retrom/internal/transport/netplay/capability"
)

type ParticipantPreparation struct {
	repository PreparationRepository
	signer     CredentialSigner
	aborter    PreparationAborter
	now        func() time.Time
	mu         sync.Mutex
}

func NewParticipantPreparation(
	repository PreparationRepository,
	signer CredentialSigner,
	aborter PreparationAborter,
	now func() time.Time,
) *ParticipantPreparation {
	return &ParticipantPreparation{repository: repository, signer: signer, aborter: aborter, now: now}
}

func (service *ParticipantPreparation) Launch(
	ctx context.Context,
	launcher NetplayLauncher,
	request PreparationRequest,
) (ParticipantLaunchResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	spec, err := service.repository.Snapshot(ctx, request.RoomID, request.SessionID, request.ProfileID)
	if err != nil {
		return ParticipantLaunchResult{}, fmt.Errorf("netplay/read preparation: %w", err)
	}
	if spec.Control.State == "FINISHED" || spec.Control.State == "FAILED" || !slices.Contains(
		[]string{"LOCKED", "LAUNCH_READY", "RUNTIME_READY"},
		spec.Peer.State,
	) {
		return ParticipantLaunchResult{}, ErrRoomConflict
	}
	generation := spec.Peer.CredentialGeneration
	if generation == 0 {
		generation = 1
	}
	credential, err := IssueParticipantCredential(service.signer, request.SessionID, request.ProfileID, generation)
	if err != nil {
		return ParticipantLaunchResult{}, err
	}
	hash := capability.HashCapability(credential)
	created, err := launcher.CreateNetplay(
		ctx,
		launch.NetplayCreateRequest{
			RoomID:                  request.RoomID,
			SessionID:               request.SessionID,
			ProfileID:               request.ProfileID,
			PlayerNo:                spec.Peer.PlayerNo,
			GameID:                  spec.GameID,
			GameVariantID:           spec.VariantID,
			ProviderID:              spec.ProviderID,
			TargetID:                spec.TargetID,
			BundleSHA256:            spec.BundleSHA256,
			ReturnTo:                "/netplay/rooms/" + request.RoomID,
			ClientCapabilities:      request.Capabilities,
			CredentialGeneration:    generation,
			NetplayCredentialSHA256: hash[:],
		},
	)
	if err != nil {
		return ParticipantLaunchResult{}, service.Fail(
			ctx,
			request.RoomID,
			request.SessionID,
			fmt.Errorf("netplay/create participant launch: %w", err),
		)
	}
	if err := service.record(ctx, request, generation); err != nil {
		return ParticipantLaunchResult{}, service.Fail(ctx, request.RoomID, request.SessionID, err)
	}
	return ParticipantLaunchResult{
		Launch:           created,
		RoomCapability:   capability.EncodeCapability(credential),
		CredentialExpiry: service.now().Add(8 * time.Hour).UnixMilli(),
	}, nil
}

func (service *ParticipantPreparation) record(ctx context.Context, request PreparationRequest, generation int64) error {
	now := service.now().UnixMilli()
	err := service.repository.WithPreparation(ctx, func(scope PreparationScope) error {
		before, err := scope.Read.Snapshot(ctx, request.RoomID, request.SessionID, request.ProfileID)
		if err != nil {
			return fmt.Errorf("netplay/read prepared participant: %w", err)
		}
		if before.Peer.CredentialGeneration != generation || before.Peer.State == "LOCKED" || before.Peer.State == "LEFT" {
			return ErrRoomConflict
		}
		events := make([]SessionEvent, 0, 2)
		if !before.LaunchRecorded {
			event := stateEvent("PARTICIPANT_STATE_CHANGED", "LOCKED", "LAUNCH_READY", "")
			event.ActorID = &request.ProfileID
			event.PlayerNo = &before.Peer.PlayerNo
			events = append(events, event)
		}
		advance := before.Locked == 0 && before.Control.State == "PREPARING"
		if advance {
			events = append(events, stateEvent("SESSION_STATE_CHANGED", "PREPARING", "LOADING", ""))
		}
		if len(events) == 0 {
			return nil
		}
		if err := scope.Write.Record(
			ctx,
			PreparationPlan{Before: before, Events: events, AdvanceLoading: advance, Now: now},
		); err != nil {
			return fmt.Errorf("netplay/record prepared participant: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("netplay/commit prepared participant: %w", err)
	}
	return nil
}

func (service *ParticipantPreparation) Fail(ctx context.Context, roomID, sessionID string, cause error) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := service.aborter.AbortPreparation(cleanupContext, roomID, sessionID); err != nil {
		return errors.Join(cause, fmt.Errorf("netplay/abort preparation: %w", err))
	}
	return cause
}

func (service *RoomExit) AbortPreparation(ctx context.Context, roomID, sessionID string) error {
	now := service.now().UnixMilli()
	err := service.repository.WithExit(ctx, func(scope RoomExitScope) error {
		before, err := scope.Read.Current(ctx, roomID, "")
		if errors.Is(err, ErrRoomNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("netplay/read preparation abort: %w", err)
		}
		if before.SessionID == nil || *before.SessionID != sessionID {
			return nil
		}
		return service.finish(ctx, scope.Write, before, nil, "PREPARE_FAILED", now)
	})
	if err != nil {
		return fmt.Errorf("netplay/abort original preparation: %w", err)
	}
	return nil
}
