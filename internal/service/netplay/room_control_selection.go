package netplay

import (
	"context"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/netplay"
)

func (service *RoomControl) SelectGame(
	ctx context.Context,
	roomID, actorID, gameID, profileID string,
	version int64,
) (model.Room, error) {
	request := roomMutation{
		roomID:   roomID,
		actorID:  actorID,
		version:  version,
		hostOnly: true,
		states:   []string{model.RoomStateDraft, model.RoomStateWaiting},
	}
	return service.mutate(ctx, request, func(scope model.RoomControlScope, before model.RoomControlSnapshot, now int64) error {
		selection, err := service.selection(ctx, scope, gameID, profileID)
		if err != nil {
			return err
		}
		for _, member := range before.Occupants {
			if member.PlayerNo > selection.MaxPlayers {
				return model.ErrInvalidSeat
			}
		}
		data, err := json.Marshal(struct {
			SchemaVersion int `json:"schemaVersion"`
			PlayerCount   int `json:"playerCount"`
		}{1, selection.MaxPlayers})
		if err != nil {
			return fmt.Errorf("netplay/selection event: %w", err)
		}
		player := 1
		err = scope.Write.Select(
			ctx,
			model.RoomSelectionPlan{
				Before:    before,
				Selection: selection,
				Evidence: model.RoomControlEvidence{
					ActorID:     actorID,
					Type:        "GAME_SELECTED",
					PlayerNo:    &player,
					Data:        data,
					Now:         now,
					ExpiresAtMS: now + service.waitingIdle.Milliseconds(),
				},
			},
		)
		if err != nil {
			return fmt.Errorf("netplay/select game: %w", err)
		}
		return nil
	})
}

func (service *RoomControl) ClearGame(ctx context.Context, roomID, actorID string, version int64) (model.Room, error) {
	request := roomMutation{
		roomID:   roomID,
		actorID:  actorID,
		version:  version,
		hostOnly: true,
		states:   []string{model.RoomStateWaiting},
	}
	return service.mutate(ctx, request, func(scope model.RoomControlScope, before model.RoomControlSnapshot, now int64) error {
		player := 1
		err := scope.Write.Clear(
			ctx,
			model.RoomClearPlan{
				Before: before,
				Evidence: model.RoomControlEvidence{
					ActorID:     actorID,
					Type:        "GAME_CLEARED",
					PlayerNo:    &player,
					Data:        []byte(`{"schemaVersion":1}`),
					Now:         now,
					ExpiresAtMS: now + service.draftIdle.Milliseconds(),
				},
			},
		)
		if err != nil {
			return fmt.Errorf("netplay/clear game: %w", err)
		}
		return nil
	})
}
