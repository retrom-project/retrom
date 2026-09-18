package netplay

import (
	"encoding/json"
	"fmt"
)

// ValidateRoomCapacity checks whether a new room can be created.
func ValidateRoomCapacity(capacity RoomCapacity, maximum int) error {
	if capacity.HostActive {
		return ErrRoomConflict
	}
	if capacity.Active >= maximum {
		return ErrCapacity
	}
	return nil
}

// ValidateSeat checks whether the requested seat assignment is allowed
// given the current room snapshot. This is a pure validation function.
func ValidateSeat(before RoomControlSnapshot, playerNo int, actorID string) error {
	if before.Selection == nil {
		return ErrProfileStale
	}
	if playerNo > before.Selection.MaxPlayers {
		return ErrInvalidSeat
	}
	if before.Member != nil {
		if before.Member.Ready {
			return ErrRoomConflict
		}
		if before.Member.Role == "HOST" {
			return ErrForbidden
		}
	}
	for _, member := range before.Occupants {
		if member.PlayerNo == playerNo && member.ProfileID != actorID {
			return ErrSeatTaken
		}
	}
	return nil
}

// BuildSeatPlan constructs the seat mutation plan from the room snapshot
// and pre-generated values. The newMemberID is used when the actor is not
// yet a member; idleMS sets the evidence expiration.
func BuildSeatPlan(
	before RoomControlSnapshot,
	actorID string,
	playerNo int,
	newMemberID string,
	nowMS, idleMS int64,
) (RoomSeatPlan, error) {
	memberID, eventType := "", "SEAT_CHANGED"
	var fromPlayer *int
	if before.Member != nil {
		memberID = before.Member.ID
		player := before.Member.PlayerNo
		fromPlayer = &player
	} else {
		eventType = "MEMBER_JOINED"
		memberID = newMemberID
	}
	data, err := json.Marshal(struct {
		SchemaVersion int  `json:"schemaVersion"`
		ToPlayerNo    int  `json:"toPlayerNo"`
		FromPlayerNo  *int `json:"fromPlayerNo,omitempty"`
	}{1, playerNo, fromPlayer})
	if err != nil {
		return RoomSeatPlan{}, fmt.Errorf("netplay/seat event: %w", err)
	}
	return RoomSeatPlan{
		Before:   before,
		MemberID: memberID,
		PlayerNo: playerNo,
		Evidence: RoomControlEvidence{
			ActorID:     actorID,
			Type:        eventType,
			PlayerNo:    &playerNo,
			Data:        data,
			Now:         nowMS,
			ExpiresAtMS: nowMS + idleMS,
		},
	}, nil
}
