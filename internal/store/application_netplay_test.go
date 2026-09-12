package store

import (
	"errors"
	"testing"

	"retrom/internal/recordstore"
)

func TestActiveNetplayRoomRequiresHostAfterApplicationUpdate(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	_, err := recordstore.CreateNetplayRooms(t.Context(), db, `INSERT INTO netplay_rooms(
 id,host_profile_id,state,selected_game_id,selected_game_variant_id,netplay_profile_id,
 profile_digest,max_players,expires_at_ms,created_at_ms,updated_at_ms
 ) VALUES('hostless','current-profile','WAITING','current-game-a','current-variant-a','current-profile-v1',
 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',2,100,1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = recordstore.UpdateNetplayRooms(t.Context(), db, recordstore.Update{
		Set: "updated_at_ms=2",
		Scope: recordstore.Scope{
			Where: "id='hostless'",
		},
	})
	if !errors.Is(err, recordstore.ErrInvariant) {
		t.Fatalf("hostless active room update error = %v", err)
	}
}
