package netplay

import (
	"context"
)

func (service *Service) ExpireRooms(ctx context.Context) error {
	if err := service.components.Maintenance.Expire(ctx); err != nil {
		return applicationError("expire rooms", err)
	}
	return nil
}

func (service *Service) Recover(ctx context.Context, reason string) error {
	if err := service.components.Maintenance.Recover(ctx, reason); err != nil {
		return applicationError("recover rooms", err)
	}
	return nil
}

func (service *Service) Events(ctx context.Context, roomID string, after int64, limit int) ([]Event, error) {
	events, err := service.components.Events.Events(ctx, roomID, after, limit)
	if err != nil {
		return nil, applicationError("read events", err)
	}
	return events, nil
}
