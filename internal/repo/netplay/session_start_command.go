package netplay

import (
	"context"
	"fmt"

	"retrom/internal/model/netplay"
	"retrom/internal/repo/dbexec"
)

func (repository *SessionStart) CommitSessionStart(
	ctx context.Context, cmd netplay.SessionStartCommand,
) (netplay.Room, error) {
	var result netplay.Room
	err := dbexec.Immediate(ctx, repository.database, func(executor dbexec.Executor) error {
		records := roomControlRecords{executor}
		before, err := records.Current(ctx, cmd.RoomID, cmd.HostID)
		if err != nil {
			return fmt.Errorf("netplay/start room snapshot: %w", err)
		}
		if before.HostID != cmd.HostID {
			return netplay.ErrForbidden
		}
		if before.Version != cmd.ExpectedVersion {
			return netplay.ErrPrecondition
		}
		if before.State != netplay.RoomStateWaiting {
			return netplay.ErrRoomConflict
		}
		writes := sessionStartRecords{executor: executor}
		sessionNo, err := writes.NextNumber(ctx, cmd.RoomID)
		if err != nil {
			return err
		}
		plan := netplay.SessionStartPlan{
			Before:    before,
			SessionID: cmd.SessionID,
			SessionNo: sessionNo,
			Profile:   cmd.FrozenProfile,
			Members:   cmd.Members,
			SeatMask:  cmd.SeatMask,
			Now:       cmd.NowMS,
			Event:     cmd.Event,
		}
		result, err = writes.Insert(ctx, plan)
		if err != nil {
			return fmt.Errorf("netplay/start session: %w", err)
		}
		return nil
	})
	if err != nil {
		return netplay.Room{}, err
	}
	return result, nil
}
