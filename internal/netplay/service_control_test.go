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
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func openNetplayTestDatabase(ctx context.Context, t *testing.T, now func() time.Time) *store.DB {
	t.Helper()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), now)
	testassert.False(t, err != nil, err)
	if err := testsupport.SeedPlatformInstances(ctx, database.SQL); err != nil {
		cleanup.Error("close", database.Close())
		t.Fatal(err)
	}
	return database
}

func TestRoomCreationCapacityAndDraftExpiryAreTransactional(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	defer func() { cleanup.Error("close", database.Close()) }()
	profiles := []string{"01980000-0000-7000-8000-000000000001", "01980000-0000-7000-8000-000000000002"}
	for index, profileID := range profiles {
		if _, err := database.SQL.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`, profileID, "Player", int64(index)); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(database.SQL, nil, nil, Options{
		MaxActiveRooms: 1, DraftIdle: time.Minute, WaitingIdle: 2 * time.Minute, ReconnectLease: 10 * time.Second,
	}, func() time.Time { return now })
	created, err := service.CreateRoom(ctx, profiles[0])
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.State != RoomStateDraft }, func() bool { return created.Version != 1 }, func() bool { return created.SelfMemberID == nil }, func() bool { return len(created.Members) != 1 }), "created room = %#v, %v", created, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return expired.State != "EXPIRED" }, func() bool { return expired.EndReason == nil }, func() bool { return *expired.EndReason != "HARD_EXPIRED" }, func() bool { return expired.EndedAtMS == nil }), "expired room = %#v, %v", expired, err)
	var eventType string
	if err := database.SQL.QueryRowContext(context.Background(), `SELECT event_type FROM netplay_events WHERE room_id=? ORDER BY id DESC LIMIT 1`, created.RoomID).Scan(&eventType); err != nil || eventType != "ROOM_EXPIRED" {
		t.Fatalf("expiry event = %q, %v", eventType, err)
	}
}

func TestAcceptanceNP011SeventeenthActiveRoomIsRejectedWithoutEviction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	defer func() { cleanup.Error("close", database.Close()) }()
	service := NewService(database.SQL, nil, nil, Options{MaxActiveRooms: 16, DraftIdle: time.Hour}, func() time.Time { return now })
	rooms := make([]string, 0, 16)
	for index := 0; index < 17; index++ {
		profileID := fmt.Sprintf("01980000-0000-7000-8000-%012x", index+1)
		if _, err := database.SQL.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`, profileID, "Player", now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		room, err := service.CreateRoom(ctx, profileID)
		if index < 16 {
			testassert.Falsef(t, err != nil, "room %d: %v", index+1, err)
			rooms = append(rooms, room.RoomID)
		} else {
			testassert.Truef(t, errors.Is(err, ErrCapacity), "17th room error = %v", err)
		}
	}
	for _, roomID := range rooms {
		var state string
		if err := database.SQL.QueryRowContext(context.Background(), `SELECT state FROM netplay_rooms WHERE id=?`, roomID).Scan(&state); err != nil || state != RoomStateDraft {
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

func TestGamePageBoundsInitialCatalogWorkAndUsesStableCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	defer func() { cleanup.Error("close", database.Close()) }()
	if _, err := database.SQL.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('paging-profile','Player',?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(context.Background(), `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 205; index++ {
		gameID := fmt.Sprintf("01980000-0000-7000-8100-%012x", index)
		metadataID := fmt.Sprintf("01980000-0000-7000-8200-%012x", index)
		contentID := fmt.Sprintf("01980000-0000-7000-8300-%012x", index)
		if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,release_year,source_kind,created_at_ms)
VALUES(?,?,?,'','','','',NULL,NULL,'ADMIN_EDIT',?)
`, metadataID, gameID, fmt.Sprintf("Game %03d", index), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms)
VALUES(?,?,'ADMIN_REPLACE',?,'{}',?,?)
`, contentID, gameID, gameID, strings.Repeat("7", 64), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms)
VALUES(?,'01980000-0000-7000-8000-000000000009','PUBLISHED',?,?,?,1,?,?)
`, gameID, metadataID, contentID, fmt.Sprintf("game %03d", index), now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	service := NewService(database.SQL, nil, nil, Options{}, func() time.Time { return now })
	first, hasMore, err := service.GamePage(ctx, "paging-profile", "ALL", "", "", 100)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !hasMore }, func() bool { return len(first) != 100 }), "first game page = len %d more %t error=%v", len(first), hasMore, err)
	testassert.Falsef(t, testassert.Any(func() bool { return first[0].Title != "Game 000" }, func() bool { return first[99].Title != "Game 099" }), "first game page bounds = %q/%q", first[0].Title, first[99].Title)
	second, hasMore, err := service.GamePage(
		ctx, "paging-profile", "ALL", strings.ToLower(first[99].Title), first[99].GameID, 100,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !hasMore }, func() bool { return len(second) != 100 }), "second game page = len %d more %t error=%v", len(second), hasMore, err)
	testassert.Falsef(t, testassert.Any(func() bool { return second[0].Title != "Game 100" }, func() bool { return second[99].Title != "Game 199" }), "second game page bounds = %q/%q", second[0].Title, second[99].Title)
	third, hasMore, err := service.GamePage(
		ctx, "paging-profile", "ALL", strings.ToLower(second[99].Title), second[99].GameID, 100,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return hasMore }, func() bool { return len(third) != 5 }), "third game page = len %d more %t error=%v", len(third), hasMore, err)
	testassert.Falsef(t, testassert.Any(func() bool { return third[0].Title != "Game 200" }, func() bool { return third[4].Title != "Game 204" }), "third game page bounds = %q/%q", third[0].Title, third[4].Title)
}

func TestCoreProfilesIgnorePerGameContentIdentity(t *testing.T) {
	t.Parallel()
	manifest, err := os.ReadFile(filepath.Join("..", "..", "data", "netplay", "v1", "manifest.json"))
	testassert.False(t, err != nil, err)
	registry, err := parseRegistry(manifest, fixtureDependencySet())
	testassert.False(t, err != nil, err)
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
			testassert.Truef(t, ok, "profile %q missing", test.profileID)
			contentAllowed, artifactMatches := service.matchesCoreProfile(eligibilityRow{
				coreID: test.coreID, emulatorVersion: "4.2.3", artifactSHA: test.artifactSHA,
				artifactEnabled: 1, contentKind: "SINGLE_FILE", logicalName: test.logicalName,
			}, profile)
			testassert.Falsef(t, testassert.Any(func() bool { return !contentAllowed }, func() bool { return !artifactMatches }), "arbitrary %s content did not match core profile", test.coreID)
			contentAllowed, artifactMatches = service.matchesCoreProfile(eligibilityRow{
				coreID: test.coreID, emulatorVersion: "4.2.3", artifactSHA: test.artifactSHA,
				artifactEnabled: 1, contentKind: "MULTI_DISC_M3U_V1",
			}, profile)
			testassert.Falsef(t, testassert.Any(func() bool { return contentAllowed }, func() bool { return artifactMatches }), "unsupported %s content kind matched core profile", test.coreID)
			contentAllowed, artifactMatches = service.matchesCoreProfile(eligibilityRow{
				coreID: test.coreID, emulatorVersion: "4.2.3", artifactSHA: strings.Repeat("f", 64),
				artifactEnabled: 1, contentKind: "SINGLE_FILE",
			}, profile)
			testassert.Falsef(t, testassert.Any(func() bool { return !contentAllowed }, func() bool { return artifactMatches }), "drifted %s artifact matched core profile", test.coreID)
		})
	}
}

func TestArcadeV2EligibilityRequiresTheLockedDependencyBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	defer func() { cleanup.Error("close", database.Close()) }()
	const (
		gameID      = "01980000-0000-7000-8400-000000000001"
		metadataID  = "01980000-0000-7000-8400-000000000002"
		contentID   = "01980000-0000-7000-8400-000000000003"
		variantID   = "01980000-0000-7000-8400-000000000004"
		revisionID  = "01980000-0000-7000-8400-000000000005"
		artifactID  = "01980000-0000-7000-8400-000000000006"
		datID       = "01980000-0000-7000-8400-000000000007"
		contentBlob = "01980000-0000-7000-8400-000000000008"
		biosBlob    = "01980000-0000-7000-8400-000000000009"
	)
	snapshot := fmt.Sprintf(`{"schemaVersion":2,"machine":"child","datVersionId":%q,"closure":[{"machine":"child","kind":"CONTENT","requiredBy":null,"depth":0},{"machine":"bios","kind":"BIOS_OR_BASE","requiredBy":"child","depth":1}],"dependencies":[{"kind":"BIOS_OR_BASE","machine":"bios","requiredBy":"child","depth":1,"expectedLogicalName":"bios.zip","state":"SATISFIED_EXTERNAL","requiredEntryCount":1,"requiredEntries":["bios.bin"]}],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`, datID)
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
		{`INSERT INTO game_metadata_revisions(id,game_id,title,description,developer,publisher,genre,players,source_kind,created_at_ms) VALUES(?,?,'Arcade fixture','','','','',2,'ADMIN_EDIT',?)`, []any{metadataID, gameID, now.UnixMilli()}},
		{`INSERT INTO game_content_revisions(id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms) VALUES(?,?,'ADMIN_REPLACE','arcade-fixture','{}',?,?)`, []any{contentID, gameID, strings.Repeat("1", 64), now.UnixMilli()}},
		{`INSERT INTO games(id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,search_text,version,created_at_ms,updated_at_ms) VALUES(?,'01980000-0000-7000-8000-000000000006','PUBLISHED',?,?,'arcade fixture',1,?,?)`, []any{gameID, metadataID, contentID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES(?,?,1,?,?,?,'application/octet-stream',?)`, []any{contentBlob, strings.Repeat("2", 64), strings.Repeat("3", 32), strings.Repeat("4", 40), strings.Repeat("5", 8), now.UnixMilli()}},
		{`INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES(?,?,1,?,?,?,'application/octet-stream',?)`, []any{biosBlob, strings.Repeat("6", 64), strings.Repeat("7", 32), strings.Repeat("8", 40), strings.Repeat("9", 8), now.UnixMilli()}},
		{`INSERT INTO game_content_files(game_content_revision_id,role,logical_name,blob_id,sort_order) VALUES(?,'CONTENT','child.zip',?,0)`, []any{contentID, contentBlob}},
		{`INSERT INTO core_artifacts(id,core_id,emulatorjs_version,bundle_version,flavor,relative_path,size_bytes,sha256,provenance_json,compatibility_config_json,enabled,version,created_at_ms,updated_at_ms) VALUES(?,'fbneo','4.2.3','test','WASM','cores/fbneo.data',1,?,'{}','{}',1,1,?,?)`, []any{artifactID, strings.Repeat("a", 64), now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,sha256,parser_version,compatibility_status,parse_status,is_active,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms) VALUES(?,'fbneo',?,'BUILTIN','data/dat/fbneo.xml',?,'1','MATCHED','READY',1,1,?,?,?,?)`, []any{datID, artifactID, strings.Repeat("b", 64), now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO game_variants(id,game_id,core_id,current_revision_id,version,created_at_ms,updated_at_ms) VALUES(?,?,'fbneo',NULL,1,?,?)`, []any{variantID, gameID, now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO game_variant_revisions(id,game_variant_id,game_content_revision_id,core_artifact_id,dat_version_id,validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms) VALUES(?,?,?,?,?,?,1,'READY','READY',?,?)`, []any{revisionID, variantID, contentID, artifactID, datID, strings.Repeat("c", 64), snapshot, now.UnixMilli()}},
		{`UPDATE game_variants SET current_revision_id=? WHERE id=?`, []any{revisionID, variantID}},
		{`INSERT INTO variant_dependencies(game_variant_revision_id,kind,logical_archive,dat_version_id,source_machine_name,required_entries_json,state,created_at_ms) VALUES(?,'BIOS_OR_BASE','bios.zip',?,'bios','["bios.bin"]','SATISFIED_EXTERNAL',?)`, []any{revisionID, datID, now.UnixMilli()}},
		{`INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order) VALUES(?,'BIOS_BUNDLE','bios.zip',?,0)`, []any{revisionID, biosBlob}},
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
	row := eligibilityRow{
		revisionID: revisionID, logicalName: "child.zip", dependencyJSON: snapshot,
		datVersionID: sql.NullString{String: datID, Valid: true},
	}
	runnable, err := service.arcadeDependencySnapshotRunnable(ctx, row)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !runnable }), "schema-v2 Arcade revision runnable=%t error=%v", runnable, err)
	row.dependencyJSON = `{"schemaVersion":1,"bios":[]}`
	runnable, err = service.arcadeDependencySnapshotRunnable(ctx, row)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return runnable }), "legacy Arcade revision runnable=%t error=%v", runnable, err)
}

func TestHostCannotClaimGuestSeatThroughTheService(t *testing.T) {
	t.Parallel()
	if err := validateSeatMember(sql.NullString{String: "HOST", Valid: true}, sql.NullInt64{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("host seat mutation error = %v", err)
	}
}

func TestAuthenticateSocketDistinguishesForbiddenFromDatabaseFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openNetplayTestDatabase(ctx, t, time.Now)
	service := NewService(database.SQL, nil, nil, Options{}, time.Now)
	if _, err := service.AuthenticateSocket(ctx, "missing", "missing", "invalid"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing socket participant = %v", err)
	}
	if err := database.SQL.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateSocket(ctx, "missing", "missing", "invalid"); err == nil || errors.Is(err, ErrForbidden) {
		t.Fatalf("database failure was collapsed to forbidden: %v", err)
	}
}

func TestResyncCausesAcceptOnlyTheirDeclaredSessionStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cause resyncCause
		state string
		valid bool
	}{
		{resyncReconnect, "RUNNING", true},
		{resyncReconnect, "PAUSED_RECONNECT", true},
		{resyncReconnect, "RESYNCHRONIZING", false},
		{resyncHash, "RUNNING", true},
		{resyncHash, "PAUSED_RECONNECT", false},
		{resyncHost, "PAUSED_RECONNECT", true},
		{resyncHost, "RUNNING", false},
		{resyncCause("UNKNOWN"), "RUNNING", false},
	}
	for _, test := range tests {
		if got := validResyncSource(test.cause, test.state); got != test.valid {
			t.Fatalf("validResyncSource(%q, %q) = %v, want %v", test.cause, test.state, got, test.valid)
		}
	}
}

func TestPrepareFailureReturnsRoomToWaitingAndClearsReady(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	defer func() { cleanup.Error("close", database.Close()) }()
	hostID := "01980000-0000-7000-8000-000000000001"
	guestID := "01980000-0000-7000-8000-000000000002"
	for _, profileID := range []string{hostID, guestID} {
		if _, err := database.SQL.ExecContext(context.Background(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,?)`, profileID, "Player", now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(database.SQL, nil, nil, Options{
		MaxActiveRooms: 16, DraftIdle: time.Minute, WaitingIdle: 2 * time.Minute, ReconnectLease: 10 * time.Second,
	}, func() time.Time { return now })
	room, err := service.CreateRoom(ctx, hostID)
	testassert.False(t, err != nil, err)
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
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(context.Background(), `PRAGMA defer_foreign_keys=ON`); err != nil {
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
		if _, err := transaction.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'GUEST',2,1,1,?,?)
`, guestMemberID, room.RoomID, guestID, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `UPDATE netplay_room_members SET ready=1 WHERE room_id=? AND role='HOST'`, room.RoomID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(context.Background(), `
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
		if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO netplay_session_participants(netplay_session_id,profile_id,room_member_id,player_no,state,credential_generation,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'LOCKED',0,1,?,?)
`, sessionID, participant.profileID, participant.memberID, participant.playerNo, now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := transaction.ExecContext(context.Background(), `UPDATE netplay_rooms SET state='STARTING',current_session_id=? WHERE id=?`, sessionID, room.RoomID); err != nil {
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
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return updated.State != RoomStateWaiting }, func() bool { return len(updated.Members) != 2 }, func() bool { return updated.Members[0].Ready }, func() bool { return updated.Members[1].Ready }, func() bool { return updated.CurrentSession != nil }), "room after prepare failure = %#v", updated)
	var sessionState, reason string
	var left int
	err = database.SQL.QueryRowContext(context.Background(),
		`SELECT state,end_reason FROM netplay_sessions WHERE id=?`, sessionID).Scan(&sessionState, &reason)
	testassert.False(t, err != nil, err)
	err = database.SQL.QueryRowContext(context.Background(),
		`SELECT count(*) FROM netplay_session_participants WHERE netplay_session_id=? AND state='LEFT'`, sessionID,
	).Scan(&left)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return sessionState != "FAILED" }, func() bool { return reason != "PREPARE_FAILED" }, func() bool { return left != 2 }), "session after prepare failure = state %q reason %q left %d", sessionState, reason, left)
	manifest, err := os.ReadFile(filepath.Join("..", "..", "data", "netplay", "v1", "manifest.json"))
	testassert.False(t, err != nil, err)
	service.registry, err = parseRegistry(manifest, fixtureDependencySet())
	testassert.False(t, err != nil, err)
	games, err := service.Games(ctx, hostID, "SUPPORTED")
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return len(games) != 1 }, func() bool { return games[0].GameID != gameID }, func() bool { return len(games[0].NetplayProfiles) != 1 }, func() bool { return games[0].NetplayProfiles[0].ID != "fceumm-423-v1" }, func() bool { return games[0].BlockerCode != nil }), "eligible games = %#v", games)
	eligible, err := service.eligibleProfiles(ctx, gameID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(eligible) != 1 }), "eligible profile for retry = %#v, %v", eligible, err)
	_, retryDigest, err := service.registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: eligible[0].Manifest, CoreArtifactID: eligible[0].CoreArtifactID,
		GameVariantRevisionID:  eligible[0].VariantRevisionID,
		DependencySnapshotJSON: eligible[0].DependencySnapshotJSON,
		DefaultCoreOptions:     eligible[0].DefaultCoreOptions,
	})
	testassert.False(t, err != nil, err)
	if _, err := database.SQL.ExecContext(context.Background(), `
UPDATE netplay_rooms SET netplay_profile_id=?,profile_digest=?,version=version+1 WHERE id=?
`, eligible[0].Manifest.ID, retryDigest, room.RoomID); err != nil {
		t.Fatal(err)
	}
	updated, err = service.Room(ctx, room.RoomID, hostID)
	testassert.False(t, err != nil, err)
	retryContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	updated, err = service.SetReady(retryContext, room.RoomID, hostID, true, updated.Version)
	testassert.Falsef(t, err != nil, "host ready after preparation retry: %v", err)
	updated, err = service.SetReady(retryContext, room.RoomID, guestID, true, updated.Version)
	testassert.Falsef(t, err != nil, "guest ready after preparation retry: %v", err)
	updated, err = service.Start(retryContext, room.RoomID, hostID, updated.Version)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return updated.State != RoomStateStarting }, func() bool { return updated.CurrentSession == nil }), "restart after preparation failure = %#v, %v", updated, err)
}
