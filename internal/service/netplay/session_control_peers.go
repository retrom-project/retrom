package netplay

import (
	"context"
	"fmt"

	model "retrom/internal/model/netplay"
)

func (service *SessionControl) Disconnected(
	ctx context.Context, identity model.PeerIdentity,
) error {
	err := service.repository.CommitDisconnected(ctx, model.DisconnectedCommand{
		Identity: identity,
		NowMS:    service.now().UnixMilli(),
		LeaseMS:  service.reconnectLease.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("netplay/session control: %w", err)
	}
	return nil
}

func (service *SessionControl) RuntimeReady(
	ctx context.Context, identity model.PeerIdentity,
) (bool, error) {
	allReady, err := service.repository.CommitRuntimeReady(ctx, model.RuntimeReadyCommand{
		Identity: identity,
		NowMS:    service.now().UnixMilli(),
	})
	if err != nil {
		return false, fmt.Errorf("netplay/session control: %w", err)
	}
	return allReady, nil
}
