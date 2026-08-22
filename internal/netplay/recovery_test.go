package netplay

import (
	"context"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestAcceptanceNP011RecoveryClosesRunningSessionRoomAndLaunch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	defer func() { cleanup.Error("close", database.Close()) }()
	const (
		profileID  = "01980000-0000-7000-8000-00000000e001"
		gameID     = "01980000-0000-7000-8000-00000000e002"
		metadataID = "01980000-0000-7000-8000-00000000e003"
		contentID  = "01980000-0000-7000-8000-00000000e004"
		variantID  = "01980000-0000-7000-8000-00000000e005"
		revisionID = "01980000-0000-7000-8000-00000000e006"
		artifactID = "01980000-0000-7000-8000-00000000e011"
		roomID     = "01980000-0000-7000-8000-00000000e007"
		memberID   = "01980000-0000-7000-8000-00000000e008"
		sessionID  = "01980000-0000-7000-8000-00000000e009"
		launchID   = "01980000-0000-7000-8000-00000000e010"
		playID     = "01980000-0000-7000-8000-00000000e012"
	)
	tx, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(tx)
	if _, err := tx.ExecContext(context.Background(), `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`, []any{profileID, "Host", now.UnixMilli()}},
		{`INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,source_kind,created_at_ms) VALUES(?,?,'Recovery','','','','',2,'ADMIN_EDIT',?)`, []any{metadataID, gameID, now.UnixMilli()}},
		{`INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms) VALUES(?,?,'ADMIN_REPLACE','recovery','{}',?,?)`, []any{contentID, gameID, strings.Repeat("1", 64), now.UnixMilli()}},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms) VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='nes/fceumm'),'PUBLISHED',?,?,'recovery',1,?,?)`, []any{gameID, metadataID, contentID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms) VALUES(?,'fceumm','4.2.3','test','WASM','cores/test.data',1,?,'{}','{}',1,1,?,?)`, []any{artifactID, strings.Repeat("4", 64), now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms) VALUES(?,?,'fceumm',NULL,1,?,?)`, []any{variantID, gameID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms) VALUES(?,?,?,?,?,1,'READY','READY','{"schemaVersion":1,"bios":[]}',?)`, []any{revisionID, variantID, contentID, artifactID, strings.Repeat("2", 64), now.UnixMilli()}},
		{`UPDATE game_variants SET current_revision_id=? WHERE id=?`, []any{revisionID, variantID}},
		{`INSERT INTO netplay_rooms(id,host_profile_id,state,selected_game_id,selected_game_variant_revision_id,netplay_profile_id,profile_digest,max_players,version,expires_at_ms,created_at_ms,updated_at_ms) VALUES(?,?,'WAITING',?,?,'fixture',?,2,1,?,?,?)`, []any{roomID, profileID, gameID, revisionID, strings.Repeat("3", 64), now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms) VALUES(?,?,?,'HOST',1,1,1,?,?)`, []any{memberID, roomID, profileID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO netplay_sessions(id,room_id,session_no,state,game_id,game_variant_revision_id,core_artifact_id,netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,version,created_at_ms,updated_at_ms) VALUES(?,?,1,'RUNNING',?,?,?,'fixture','{}',?,2,3,1,?,?)`, []any{sessionID, roomID, gameID, revisionID, artifactID, strings.Repeat("3", 64), now.UnixMilli(), now.UnixMilli()}},
		{`UPDATE netplay_rooms SET state='RUNNING',current_session_id=? WHERE id=?`, []any{sessionID, roomID}},
		{`INSERT INTO netplay_session_participants(netplay_session_id,profile_id,room_member_id,player_no,state,credential_generation,version,created_at_ms,updated_at_ms) VALUES(?,?,?,1,'LOCKED',0,1,?,?)`, []any{sessionID, profileID, memberID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO launch_sessions(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,return_to,credential_sha256,state,bootstrap_expires_at_ms,activated_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,netplay_session_id,netplay_player_no,save_access) VALUES(?,?,?,?,?,'/netplay/rooms/'||?,zeroblob(32),'ACTIVE',?,?,?,?, ?,?,1,'NETPLAY_DISABLED')`, []any{launchID, profileID, gameID, revisionID, artifactID, roomID, now.Add(time.Minute).UnixMilli(), now.UnixMilli(), now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli(), sessionID}},
		{`INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,game_variant_revision_id,started_at_ms,last_heartbeat_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,0,0,'ACTIVE',1,?,?)`, []any{playID, launchID, profileID, gameID, revisionID, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli()}},
		{`UPDATE netplay_session_participants SET state='CONNECTED',launch_session_id=?,credential_sha256=zeroblob(32),credential_generation=1 WHERE netplay_session_id=? AND profile_id=?`, []any{launchID, sessionID, profileID}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatalf("fixture statement %q: %v", statement.query, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	service := NewService(database.SQL, nil, nil, Options{}, func() time.Time { return now })
	if err := service.Recover(ctx, "SERVER_RESTARTED"); err != nil {
		t.Fatal(err)
	}
	var roomState, roomReason, sessionState, sessionReason, launchState, playState string
	var playEndedAt int64
	err = database.SQL.QueryRowContext(context.Background(),
		`SELECT state,end_reason FROM netplay_rooms WHERE id=?`, roomID).Scan(&roomState, &roomReason)
	testassert.False(t, err != nil, err)
	err = database.SQL.QueryRowContext(context.Background(),
		`SELECT state,end_reason FROM netplay_sessions WHERE id=?`, sessionID).Scan(&sessionState, &sessionReason)
	testassert.False(t, err != nil, err)
	err = database.SQL.QueryRowContext(context.Background(),
		`SELECT state FROM launch_sessions WHERE id=?`, launchID).Scan(&launchState)
	testassert.False(t, err != nil, err)
	err = database.SQL.QueryRowContext(context.Background(),
		`SELECT state,ended_at_ms FROM play_sessions WHERE id=?`, playID).Scan(&playState, &playEndedAt)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return roomState != "ENDED" }, func() bool { return roomReason != "SERVER_RESTARTED" }, func() bool { return sessionState != "FAILED" }, func() bool { return sessionReason != "SERVER_RESTARTED" }, func() bool { return launchState != "REVOKED" }, func() bool { return playState != "ABANDONED" }, func() bool { return playEndedAt != now.UnixMilli() }), "recovery room=%s/%s session=%s/%s launch=%s play=%s/%d", roomState, roomReason, sessionState, sessionReason, launchState, playState, playEndedAt)

	const (
		finishedSessionID = "01980000-0000-7000-8000-00000000e013"
		finishedLaunchID  = "01980000-0000-7000-8000-00000000e014"
		finishedPlayID    = "01980000-0000-7000-8000-00000000e015"
	)
	tx, err = database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(tx)
	if _, err := tx.ExecContext(context.Background(), `
INSERT INTO netplay_sessions(id,room_id,session_no,state,game_id,game_variant_revision_id,core_artifact_id,
netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,version,created_at_ms,updated_at_ms)
VALUES(?,?,2,'RUNNING',?,?,?,'fixture','{}',?,2,3,1,?,?)
`, finishedSessionID, roomID, gameID, revisionID, artifactID, strings.Repeat("3", 64), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `
INSERT INTO netplay_session_participants(netplay_session_id,profile_id,room_member_id,player_no,state,
credential_generation,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,1,'LOCKED',0,1,?,?)
`, finishedSessionID, profileID, memberID, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `
INSERT INTO launch_sessions(id,profile_id,game_id,game_variant_revision_id,core_artifact_id,return_to,
credential_sha256,state,bootstrap_expires_at_ms,activated_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,
netplay_session_id,netplay_player_no,save_access)
VALUES(?,?,?,?,?,'/netplay/rooms/'||?,randomblob(32),'ACTIVE',?,?,?,?,?,?,1,'NETPLAY_DISABLED')
`, finishedLaunchID, profileID, gameID, revisionID, artifactID, roomID, now.Add(time.Minute).UnixMilli(),
		now.UnixMilli(), now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli(), finishedSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,game_variant_revision_id,started_at_ms,
last_heartbeat_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,0,0,'ACTIVE',1,?,?)
`, finishedPlayID, finishedLaunchID, profileID, gameID, revisionID, now.UnixMilli(), now.UnixMilli(),
		now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := closeNetplaySession(ctx, tx, finishedSessionID, "FINISHED", "USER_EXIT", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(context.Background(), `SELECT state,ended_at_ms FROM play_sessions WHERE id=?`, finishedPlayID).
		Scan(&playState, &playEndedAt); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(context.Background(), `SELECT state FROM launch_sessions WHERE id=?`, finishedLaunchID).Scan(&launchState); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return playState != "FINISHED" }, func() bool { return playEndedAt != now.UnixMilli() }, func() bool { return launchState != "REVOKED" }), "normal end launch=%s play=%s/%d", launchState, playState, playEndedAt)
}
