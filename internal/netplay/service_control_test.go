package netplay

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
)

func TestRoomCreationCapacityAndDraftExpiryAreTransactional(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	profiles := []string{"01980000-0000-7000-8000-000000000001", "01980000-0000-7000-8000-000000000002"}
	for index, profileID := range profiles {
		if _, err := database.SQL.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`, profileID, "Player", int64(index)); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(database.SQL, nil, nil, Options{
		MaxActiveRooms: 1, DraftIdle: time.Minute, WaitingIdle: 2 * time.Minute, ReconnectLease: 10 * time.Second,
	}, func() time.Time { return now })
	created, err := service.CreateRoom(ctx, profiles[0])
	if err != nil || created.State != RoomStateDraft || created.Version != 1 || created.SelfMemberID == nil || len(created.Members) != 1 {
		t.Fatalf("created room = %#v, %v", created, err)
	}
	if _, err := service.CreateRoom(ctx, profiles[0]); !errors.Is(err, ErrCapacity) && !errors.Is(err, ErrRoomConflict) {
		t.Fatalf("second hosted room error = %v", err)
	}
	if _, err := service.CreateRoom(ctx, profiles[1]); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
	now = now.Add(time.Minute)
	if err := service.ExpireRooms(ctx); err != nil {
		t.Fatal(err)
	}
	expired, err := service.Room(ctx, created.RoomID, profiles[0])
	if err != nil || expired.State != "EXPIRED" || expired.EndReason == nil || *expired.EndReason != "HARD_EXPIRED" || expired.EndedAtMS == nil {
		t.Fatalf("expired room = %#v, %v", expired, err)
	}
	var eventType string
	if err := database.SQL.QueryRow(`SELECT event_type FROM netplay_events WHERE room_id=? ORDER BY id DESC LIMIT 1`, created.RoomID).Scan(&eventType); err != nil || eventType != "ROOM_EXPIRED" {
		t.Fatalf("expiry event = %q, %v", eventType, err)
	}
}

func TestAcceptanceNP011SeventeenthActiveRoomIsRejectedWithoutEviction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	service := NewService(database.SQL, nil, nil, Options{MaxActiveRooms: 16, DraftIdle: time.Hour}, func() time.Time { return now })
	rooms := make([]string, 0, 16)
	for index := 0; index < 17; index++ {
		profileID := fmt.Sprintf("01980000-0000-7000-8000-%012x", index+1)
		if _, err := database.SQL.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`, profileID, "Player", now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		room, err := service.CreateRoom(ctx, profileID)
		if index < 16 {
			if err != nil {
				t.Fatalf("room %d: %v", index+1, err)
			}
			rooms = append(rooms, room.RoomID)
		} else if !errors.Is(err, ErrCapacity) {
			t.Fatalf("17th room error = %v", err)
		}
	}
	for _, roomID := range rooms {
		var state string
		if err := database.SQL.QueryRow(`SELECT state FROM netplay_rooms WHERE id=?`, roomID).Scan(&state); err != nil || state != RoomStateDraft {
			t.Fatalf("existing room %s state=%s err=%v", roomID, state, err)
		}
	}
}

func TestEligibilityBlockerUsesClosedRepairCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                                    string
		hasVariant, contentAllowed, coreAllowed bool
		want                                    string
	}{
		{name: "game unavailable", want: "GAME_UNAVAILABLE"},
		{name: "content kind", hasVariant: true, want: "CONTENT_NOT_ALLOWLISTED"},
		{name: "core", hasVariant: true, contentAllowed: true, want: "CORE_NOT_ALLOWLISTED"},
		{name: "dependency", hasVariant: true, contentAllowed: true, coreAllowed: true, want: "DEPENDENCY_STALE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := eligibilityBlocker(test.hasVariant, test.contentAllowed, test.coreAllowed); got != test.want {
				t.Fatalf("eligibilityBlocker() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCoreProfilesIgnorePerGameContentIdentity(t *testing.T) {
	t.Parallel()
	manifest, err := os.ReadFile(filepath.Join("..", "..", "data", "netplay", "v1", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := parseRegistry(manifest, fixtureDependencySet())
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{registry: registry}
	tests := []struct {
		profileID, coreID, artifactSHA, logicalName string
	}{
		{
			profileID: "fceumm-423-v1", coreID: "fceumm",
			artifactSHA: "8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493",
			logicalName: "unverified-region-build.nes",
		},
		{
			profileID: "fbneo-423-v1", coreID: "fbneo",
			artifactSHA: "315a25e0bcd61d58ee0d9e8b1dbf3740b9e0ca4b7d0726f848ce1068de73437c",
			logicalName: "another-machine.zip",
		},
	}
	for _, test := range tests {
		t.Run(test.coreID, func(t *testing.T) {
			t.Parallel()
			profile, ok := registry.Profile(test.profileID)
			if !ok {
				t.Fatalf("profile %q missing", test.profileID)
			}
			contentAllowed, artifactMatches := service.matchesCoreProfile(eligibilityRow{
				coreID: test.coreID, emulatorVersion: "4.2.3", artifactSHA: test.artifactSHA,
				artifactEnabled: 1, contentKind: "SINGLE_FILE", logicalName: test.logicalName,
			}, profile)
			if !contentAllowed || !artifactMatches {
				t.Fatalf("arbitrary %s content did not match core profile", test.coreID)
			}
			contentAllowed, artifactMatches = service.matchesCoreProfile(eligibilityRow{
				coreID: test.coreID, emulatorVersion: "4.2.3", artifactSHA: test.artifactSHA,
				artifactEnabled: 1, contentKind: "MULTI_DISC_M3U_V1",
			}, profile)
			if contentAllowed || artifactMatches {
				t.Fatalf("unsupported %s content kind matched core profile", test.coreID)
			}
			contentAllowed, artifactMatches = service.matchesCoreProfile(eligibilityRow{
				coreID: test.coreID, emulatorVersion: "4.2.3", artifactSHA: strings.Repeat("f", 64),
				artifactEnabled: 1, contentKind: "SINGLE_FILE",
			}, profile)
			if !contentAllowed || artifactMatches {
				t.Fatalf("drifted %s artifact matched core profile", test.coreID)
			}
		})
	}
}

func TestHostCannotClaimGuestSeatThroughTheService(t *testing.T) {
	t.Parallel()
	if err := validateSeatMember(sql.NullString{String: "HOST", Valid: true}, sql.NullInt64{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("host seat mutation error = %v", err)
	}
}

func TestPrepareFailureReturnsRoomToWaitingAndClearsReady(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", database.Close()) }()
	hostID := "01980000-0000-7000-8000-000000000001"
	guestID := "01980000-0000-7000-8000-000000000002"
	for _, profileID := range []string{hostID, guestID} {
		if _, err := database.SQL.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`, profileID, "Player", now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(database.SQL, nil, nil, Options{
		MaxActiveRooms: 16, DraftIdle: time.Minute, WaitingIdle: 2 * time.Minute, ReconnectLease: 10 * time.Second,
	}, func() time.Time { return now })
	room, err := service.CreateRoom(ctx, hostID)
	if err != nil {
		t.Fatal(err)
	}
	const (
		gameID            = "01980000-0000-7000-8000-000000000010"
		metadataID        = "01980000-0000-7000-8000-000000000011"
		contentID         = "01980000-0000-7000-8000-000000000012"
		artifactID        = "01980000-0000-7000-8000-000000000013"
		variantID         = "01980000-0000-7000-8000-000000000014"
		variantRevisionID = "01980000-0000-7000-8000-000000000015"
		guestMemberID     = "01980000-0000-7000-8000-000000000016"
		sessionID         = "01980000-0000-7000-8000-000000000017"
		contentBlobID     = "01980000-0000-7000-8000-000000000018"
	)
	transaction, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.Exec(`PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,created_at_ms)
VALUES(?,?,'Prepare fixture','','','','',2,NULL,'ADMIN_EDIT',?)`, []any{metadataID, gameID, now.UnixMilli()}},
		{`INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
VALUES(?,?,'ADMIN_REPLACE','prepare-fixture','{}',?,?)`, []any{contentID, gameID, strings.Repeat("1", 64), now.UnixMilli()}},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms)
VALUES(?,'01980000-0000-7000-8000-000000000009','PUBLISHED',?,?,'prepare fixture',1,?,?)`, []any{gameID, metadataID, contentID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES(?,?,32768,?,?,?,'application/octet-stream',?)`, []any{contentBlobID, strings.Repeat("9", 64), strings.Repeat("5", 32), strings.Repeat("6", 40), strings.Repeat("7", 8), now.UnixMilli()}},
		{`INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order) VALUES(?,'CONTENT','another-game.nes',?,0)`, []any{contentID, contentBlobID}},
		{`INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms)
VALUES(?,'fceumm','4.2.3','test','WASM','cores/test.data',1,?,'{}','{}',1,1,?,?)`, []any{artifactID, "8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493", now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms)
VALUES(?,?,'fceumm',NULL,1,?,?)`, []any{variantID, gameID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES(?,?,?,?,?,9001,'READY','READY','{"schemaVersion":1,"bios":[]}',?)`, []any{variantRevisionID, variantID, contentID, artifactID, strings.Repeat("3", 64), now.UnixMilli()}},
		{`UPDATE game_variants SET current_revision_id=? WHERE id=?`, []any{variantRevisionID, variantID}},
		{`UPDATE netplay_rooms SET state='WAITING',selected_game_id=?,selected_game_variant_revision_id=?,netplay_profile_id='fixture',profile_digest=?,max_players=2 WHERE id=?`, []any{gameID, variantRevisionID, strings.Repeat("4", 64), room.RoomID}},
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := transaction.Exec(`
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'GUEST',2,1,1,?,?)
`, guestMemberID, room.RoomID, guestID, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`UPDATE netplay_room_members SET ready=1 WHERE room_id=? AND role='HOST'`, room.RoomID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO netplay_sessions(id,room_id,session_no,state,game_id,game_variant_revision_id,core_artifact_id,
netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,authority_player_no,resync_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,1,'PREPARING',?,?,?,'fixture','{}',?,2,3,1,0,1,?,?)
`, sessionID, room.RoomID, gameID, variantRevisionID, artifactID, strings.Repeat("4", 64), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for _, participant := range []struct {
		profileID, memberID string
		playerNo            int
	}{
		{hostID, *room.SelfMemberID, 1}, {guestID, guestMemberID, 2},
	} {
		if _, err := transaction.Exec(`
INSERT INTO netplay_session_participants(netplay_session_id,profile_id,room_member_id,player_no,state,credential_generation,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'LOCKED',0,1,?,?)
`, sessionID, participant.profileID, participant.memberID, participant.playerNo, now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := transaction.Exec(`UPDATE netplay_rooms SET state='STARTING',current_session_id=? WHERE id=?`, sessionID, room.RoomID); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("launch preflight failed")
	if err := service.failPreparation(ctx, room.RoomID, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("failPreparation() error = %v", err)
	}
	updated, err := service.Room(ctx, room.RoomID, hostID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != RoomStateWaiting || len(updated.Members) != 2 || updated.Members[0].Ready || updated.Members[1].Ready || updated.CurrentSession != nil {
		t.Fatalf("room after prepare failure = %#v", updated)
	}
	var sessionState, reason string
	var left int
	if err := database.SQL.QueryRow(`SELECT state,end_reason FROM netplay_sessions WHERE id=?`, sessionID).Scan(&sessionState, &reason); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRow(`SELECT count(*) FROM netplay_session_participants WHERE netplay_session_id=? AND state='LEFT'`, sessionID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if sessionState != "FAILED" || reason != "PREPARE_FAILED" || left != 2 {
		t.Fatalf("session after prepare failure = state %q reason %q left %d", sessionState, reason, left)
	}
	manifest, err := os.ReadFile(filepath.Join("..", "..", "data", "netplay", "v1", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.registry, err = parseRegistry(manifest, fixtureDependencySet())
	if err != nil {
		t.Fatal(err)
	}
	games, err := service.Games(ctx, hostID, "SUPPORTED")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].GameID != gameID || len(games[0].NetplayProfiles) != 1 ||
		games[0].NetplayProfiles[0].ID != "fceumm-423-v1" || games[0].BlockerCode != nil {
		t.Fatalf("eligible games = %#v", games)
	}
	eligible, err := service.eligibleProfiles(ctx, gameID)
	if err != nil || len(eligible) != 1 {
		t.Fatalf("eligible profile for retry = %#v, %v", eligible, err)
	}
	_, retryDigest, err := service.registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: eligible[0].Manifest, CoreArtifactID: eligible[0].CoreArtifactID,
		GameVariantRevisionID:  eligible[0].VariantRevisionID,
		DependencySnapshotJSON: eligible[0].DependencySnapshotJSON,
		DefaultCoreOptions:     eligible[0].DefaultCoreOptions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
UPDATE netplay_rooms SET netplay_profile_id=?,profile_digest=?,version=version+1 WHERE id=?
`, eligible[0].Manifest.ID, retryDigest, room.RoomID); err != nil {
		t.Fatal(err)
	}
	updated, err = service.Room(ctx, room.RoomID, hostID)
	if err != nil {
		t.Fatal(err)
	}
	retryContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	updated, err = service.SetReady(retryContext, room.RoomID, hostID, true, updated.Version)
	if err != nil {
		t.Fatalf("host ready after preparation retry: %v", err)
	}
	updated, err = service.SetReady(retryContext, room.RoomID, guestID, true, updated.Version)
	if err != nil {
		t.Fatalf("guest ready after preparation retry: %v", err)
	}
	updated, err = service.Start(retryContext, room.RoomID, hostID, updated.Version)
	if err != nil || updated.State != RoomStateStarting || updated.CurrentSession == nil {
		t.Fatalf("restart after preparation failure = %#v, %v", updated, err)
	}
}
