package netplay

import (
	"context"
	model "retrom/internal/model/netplay"
	"slices"
	"time"
)

type RoomQueries struct {
	repository model.RoomQueryRepository
	now        func() time.Time
}

func NewRoomQueries(repository model.RoomQueryRepository, now func() time.Time) *RoomQueries {
	return &RoomQueries{repository: repository, now: now}
}

func (service *RoomQueries) List(
	ctx context.Context,
	profileID, view string,
	afterUpdatedAtMS int64,
	afterRoomID string,
	limit int,
) ([]model.Room, bool, error) {
	if view == "" {
		view = "active"
	}
	if (view != "active" && view != "recent") || limit < 1 || limit > 100 {
		return nil, false, model.ErrRoomConflict
	}
	ids, err := service.repository.RoomIDs(ctx, model.RoomFilter{
		ProfileID: profileID, View: view, AfterUpdatedAtMS: afterUpdatedAtMS, AfterRoomID: afterRoomID,
		Limit: limit + 1, RecentSinceMS: service.now().Add(-24 * time.Hour).UnixMilli(),
	})
	if err != nil {
		return nil, false, serviceError("list rooms", err)
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	result := make([]model.Room, 0, len(ids))
	for _, roomID := range ids {
		room, err := service.Get(ctx, roomID, profileID)
		if err != nil {
			return nil, false, serviceError("list rooms", err)
		}
		result = append(result, room)
	}
	return result, hasMore, nil
}

func (service *RoomQueries) Get(ctx context.Context, roomID, viewerProfileID string) (model.Room, error) {
	result, err := service.repository.Snapshot(ctx, roomID)
	if err != nil {
		return model.Room{}, serviceError("get room", err)
	}
	return roomForViewer(result, viewerProfileID, service.now().UnixMilli()), nil
}

func roomForViewer(result model.Room, viewerProfileID string, now int64) model.Room {
	if result.Game != nil {
		game := *result.Game
		game.Availability = game.Status
		result.Game = &game
	}
	result.ServerNowMS = now
	result.Permissions = model.RoomPermissions{}
	result.SelfMemberID = nil
	for _, member := range result.Members {
		if member.ProfileID == viewerProfileID {
			value := member.MemberID
			result.SelfMemberID = &value
			result.Permissions.Member = true
			result.Permissions.Host = member.Role == "HOST"
			break
		}
	}
	setRoomPermissions(&result)
	return result
}

func setRoomPermissions(room *model.Room) {
	waiting := room.State == model.RoomStateWaiting
	room.Permissions.CanSelect = room.Permissions.Host && (room.State == model.RoomStateDraft || waiting)
	room.Permissions.CanJoin = waiting && !room.Permissions.Member
	room.Permissions.CanReady = waiting && room.Permissions.Member
	room.Permissions.CanStart = waiting && room.Permissions.Host && RoomReady(room.Members)
	room.Permissions.CanClose = room.Permissions.Host && room.State != "ENDED" && room.State != "EXPIRED"
}

func RoomReady(members []model.RoomMember) bool {
	return len(members) >= 2 && !slices.ContainsFunc(members, func(member model.RoomMember) bool { return !member.Ready })
}
