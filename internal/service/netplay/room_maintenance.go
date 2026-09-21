package netplay

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidRecoveryReason = errors.New("netplay/recovery: invalid reason")

type ExpiryCutoffs struct {
	Now, StartingBefore, RunningBefore int64
	Limit                              int
}
type ExpiryCandidate struct {
	RoomID, HostID, State string
	Version               int64
	SessionID             *string
}
type ExpiryPlan struct {
	Before ExpiryCandidate
	Now    int64
}
type RecoveryPlan struct {
	Reason string
	Now    int64
}
type MaintenanceWriter interface {
	Expire(context.Context, ExpiryPlan) error
	Recover(context.Context, RecoveryPlan) error
}
type MaintenanceRepository interface {
	Passive(context.Context, ExpiryCutoffs) ([]ExpiryCandidate, error)
	Active(context.Context, ExpiryCutoffs) ([]ExpiryCandidate, error)
	WithMaintenance(context.Context, func(MaintenanceWriter) error) error
}
type ExpiredSessionEnder interface {
	EndExpired(context.Context, ExpiryCandidate, int64) error
}
type RoomMaintenance struct {
	repository MaintenanceRepository
	ender      ExpiredSessionEnder
	now        func() time.Time
}

func NewRoomMaintenance(
	repository MaintenanceRepository,
	ender ExpiredSessionEnder,
	now func() time.Time,
) *RoomMaintenance {
	return &RoomMaintenance{repository, ender, now}
}

func (service *RoomMaintenance) Expire(ctx context.Context) error {
	now := service.now()
	cutoffs := ExpiryCutoffs{
		Now:            now.UnixMilli(),
		StartingBefore: now.Add(-2 * time.Minute).UnixMilli(),
		RunningBefore:  now.Add(-8 * time.Hour).UnixMilli(),
		Limit:          100,
	}
	passive, err := service.repository.Passive(ctx, cutoffs)
	if err != nil {
		return fmt.Errorf("netplay/read expired rooms: %w", err)
	}
	for _, candidate := range passive {
		err := service.repository.WithMaintenance(ctx, func(writer MaintenanceWriter) error {
			if err := writer.Expire(ctx, ExpiryPlan{Before: candidate, Now: cutoffs.Now}); err != nil {
				return fmt.Errorf("netplay/expire passive room: %w", err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("netplay/commit passive expiry: %w", err)
		}
	}
	active, err := service.repository.Active(ctx, cutoffs)
	if err != nil {
		return fmt.Errorf("netplay/read expired sessions: %w", err)
	}
	for _, candidate := range active {
		if err := service.ender.EndExpired(ctx, candidate, cutoffs.Now); err != nil && !errors.Is(err, ErrRoomNotFound) {
			return fmt.Errorf("netplay/end expired session: %w", err)
		}
	}
	return nil
}

func (service *RoomMaintenance) Recover(ctx context.Context, reason string) error {
	if reason != "SERVER_RESTARTED" && reason != "RESTORE" {
		return ErrInvalidRecoveryReason
	}
	plan := RecoveryPlan{Reason: reason, Now: service.now().UnixMilli()}
	err := service.repository.WithMaintenance(ctx, func(writer MaintenanceWriter) error {
		if err := writer.Recover(ctx, plan); err != nil {
			return fmt.Errorf("netplay/recover runtime: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("netplay/commit recovery: %w", err)
	}
	return nil
}

func (service *RoomExit) EndExpired(ctx context.Context, candidate ExpiryCandidate, now int64) error {
	err := service.repository.WithExit(ctx, func(scope RoomExitScope) error {
		before, err := scope.Read.Current(ctx, candidate.RoomID, "")
		if err != nil {
			return fmt.Errorf("netplay/read expiry state: %w", err)
		}
		if before.Room.Version != candidate.Version || before.Room.State != candidate.State || !sameExpirySession(
			before.SessionID,
			candidate.SessionID,
		) {
			return nil
		}
		reason := "HARD_EXPIRED"
		switch candidate.State {
		case RoomStateStarting:
			reason = "START_TIMEOUT"
		case RoomStateRunning:
		default:
			return ErrRoomConflict
		}
		return service.finish(ctx, scope.Write, before, nil, reason, now)
	})
	if err != nil {
		return fmt.Errorf("netplay/expire active session: %w", err)
	}
	return nil
}

func sameExpirySession(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
