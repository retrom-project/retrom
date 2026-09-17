//go:build integration

package launch

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/integration/libraryimport"
	"retrom/internal/adapter/runtime/dependencies"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/foundation/cleanup"
	uploadsmodel "retrom/internal/model/uploads"
	dependencypersistence "retrom/internal/repo/dependencies"
	netplaypersistence "retrom/internal/repo/netplay"
	uploadpersistence "retrom/internal/repo/uploads"
	dependencyservice "retrom/internal/service/dependencies"
	netplayservice "retrom/internal/service/netplay"
	uploadsservice "retrom/internal/service/uploads"
	"retrom/internal/testkit/testsupport"
	"retrom/internal/transport/netplay/profile"
)

const (
	netplayHostProfile  = "01980000-0000-7000-8100-000000000001"
	netplayGuestProfile = "01980000-0000-7000-8100-000000000002"
)

type netplayLaunchFixture struct {
	service  *Service
	database *sql.DB
	now      func() time.Time
	request  NetplayCreateRequest
}

func newNetplayLaunchFixture(t *testing.T) netplayLaunchFixture {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	dir := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(dir, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close netplay fixture", database.Close()) })
	for _, id := range []string{netplayHostProfile, netplayGuestProfile} {
		mustRPGLaunchSQL(
			t,
			database.SQL,
			`INSERT INTO profiles(id,display_name,created_at_ms)VALUES(?,?,?)`,
			id,
			id,
			now().UnixMilli(),
		)
	}
	deps, err := dependencies.Load(filepath.Join("..", "..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(deps, dependencypersistence.New(database.SQL)).Bootstrap(
		t.Context(),
		now(),
	); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dir)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := testsupport.NewRuntimeBuilder(t.Context(), database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	launcher := New(database.SQL, deps, credentials, now).WithBlobStore(blobs).WithRuntimeProvider(deps.RuntimeCatalog, builder).WithPublicOrigin(

		"http://localhost:3000",
	)
	t.Cleanup(launcher.Close)
	gameID := publishNetplayROM(t, database.SQL, blobs, dir, now)
	request := startNetplayFixture(t, database.SQL, deps, now, gameID)
	return netplayLaunchFixture{launcher, database.SQL, now, request}
}

func publishNetplayROM(
	t *testing.T,
	database *sql.DB,
	blobs *blobstore.Store,
	dir string,
	now func() time.Time,
) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "public-roms", "nes-smoke", "nes-smoke.nes"))
	if err != nil {
		t.Fatal(err)
	}
	service := uploadsservice.New(uploadpersistence.New(database), blobs, dir, now)
	upload, err := service.Create(t.Context(), uploadsmodel.CreateRequest{SourceType: "FILES", Files: []uploadsmodel.FileDeclaration{
		{ClientFileID: "game", RelativePath: "Netplay.nes", SizeBytes: int64(len(contents))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := service.PutPart(
		t.Context(),
		upload.ID,
		upload.Files[0].ID,
		0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),

		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
		bytes.NewReader(contents),
	); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(t.Context(), upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForONSReviewJob(t, t.Context(), database, jobID)
	importer := libraryimport.New(database, now).WithBlobStore(blobs)
	imported, err := importer.Create(t.Context(), libraryimport.CreateRequest{
		UploadID: upload.ID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(
			t,
			database,
			"nes/fceumm",
		), MetadataProvider: "NONE",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.QueryRowContext(t.Context(), `SELECT id FROM import_items WHERE import_job_id=?`, imported.ImportJobID).Scan(
		&itemID,
	); err != nil {
		t.Fatal(err)
	}
	approved, err := importer.Approve(t.Context(), itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return approved.GameID
}

func startNetplayFixture(
	t *testing.T,
	database *sql.DB,
	deps *dependencies.Set,
	now func() time.Time,
	gameID string,
) NetplayCreateRequest {
	t.Helper()
	registry, err := profile.LoadRegistry(filepath.Join("..", "..", "..", "..", "data"), deps)
	if err != nil {
		t.Fatal(err)
	}
	creator := netplayservice.NewRoomCreation(netplaypersistence.NewRoomCreation(database), 16, time.Hour, now)
	control := netplayservice.NewRoomControl(
		netplaypersistence.NewRoomControl(database),
		registry,
		time.Hour,
		time.Hour,
		now,
	)
	room, err := creator.Create(t.Context(), netplayHostProfile)
	if err != nil {
		t.Fatal(err)
	}
	room, err = control.SelectGame(t.Context(), room.RoomID, netplayHostProfile, gameID, "fceumm-423-v1", room.Version)
	if err != nil {
		t.Fatal(err)
	}
	room, err = control.SetSeat(t.Context(), room.RoomID, netplayGuestProfile, 2, room.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{netplayHostProfile, netplayGuestProfile} {
		room, err = control.SetReady(t.Context(), room.RoomID, id, true, room.Version)
		if err != nil {
			t.Fatal(err)
		}
	}
	starter := netplayservice.NewSessionStart(netplaypersistence.NewSessionStart(database), registry, now)
	room, err = starter.Start(t.Context(), room.RoomID, netplayHostProfile, room.Version)
	if err != nil {
		t.Fatal(err)
	}
	request := NetplayCreateRequest{
		RoomID: room.RoomID, SessionID: room.CurrentSession.SessionID, ProfileID: netplayHostProfile, PlayerNo: 1,
		ReturnTo: "/netplay/rooms/" + room.RoomID, CredentialGeneration: 1,
		ClientCapabilities: Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
	}
	credential := sha256.Sum256([]byte("deterministic participant credential"))
	request.NetplayCredentialSHA256 = credential[:]
	if err := database.QueryRowContext(t.Context(), `SELECT game_id,game_variant_id,provider_id,target_id,bundle_sha256 FROM netplay_sessions WHERE id=?`, request.SessionID).
		Scan(
			&request.GameID,
			&request.GameVariantID,
			&request.ProviderID,
			&request.TargetID,
			&request.BundleSHA256,
		); err != nil {
		t.Fatal(err)
	}
	return request
}
