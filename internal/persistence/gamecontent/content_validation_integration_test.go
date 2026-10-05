//go:build integration

package gamecontent

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/libraryimport"
	dependencypersistence "retrom/internal/persistence/dependencies"
	uploadpersistence "retrom/internal/persistence/uploads"
	dependencyservice "retrom/internal/service/dependencies"
	"retrom/internal/service/gamecontent"
	"retrom/internal/service/uploads"
	"retrom/internal/testsupport"
	"retrom/internal/testsupport/importfixture"
)

func TestWASMReplacementRejectsInvalidContentWithoutRetiringCurrent(t *testing.T) {
	database, blobs, uploader, gameID, cart := publishedWASMGame(t)
	ctx := t.Context()
	saveID, launchID, _ := seedReplacementSave(t, ctx, database, blobs, gameID)
	var version int64
	var originalRecord string
	if err := dbapi.QueryRowContext(ctx, database, `SELECT g.version,f.file_record
 FROM games g JOIN game_files f ON f.game_id=g.id WHERE g.id=?`, gameID).
		Scan(&version, &originalRecord); err != nil {
		t.Fatal(err)
	}
	service := gamecontent.New(gamecontent.Dependencies{Repository: New(database), Files: blobs}, gamecontent.Options{})
	tests := []struct {
		name  string
		bytes []byte
		code  string
	}{
		{"disguised.wasm", []byte("not wasm"), "WASM4_CART_INVALID"},
		{"truncated.wasm", cart[:len(cart)-1], "WASM4_CART_INVALID"},
		{"wrong.zip", replacementZIP(t, "wrong.nes", cart), "NO_SUPPORTED_CONTENT"},
		{"same.wasm", cart, "GAME_CONTENT_UNCHANGED"},
		{"same.zip", replacementZIP(t, "nested/cart.wasm", cart), "GAME_CONTENT_UNCHANGED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upload := completeUpload(t, ctx, database, uploader, test.name, test.bytes)
			job, err := service.Schedule(ctx, gameID, upload, version)
			if err != nil {
				t.Fatal(err)
			}
			waitForJob(t, ctx, database, job.JobID, "FAILED")
			assertReplacementFailure(t, ctx, database, job.JobID, test.code, gameID, gameID, saveID)
			var currentVersion int64
			var record, variant, launch string
			err = dbapi.QueryRowContext(ctx, database, `SELECT g.version,f.file_record,v.status,l.state
 FROM games g JOIN game_files f ON f.game_id=g.id JOIN game_variants v ON v.game_id=g.id
 JOIN launch_sessions l ON l.id=? WHERE g.id=?`, launchID, gameID).
				Scan(&currentVersion, &record, &variant, &launch)
			if err != nil {
				t.Fatal(err)
			}
			if currentVersion != version || record != originalRecord || variant != "READY" || launch != "CREATED" {
				t.Fatalf("failed replacement changed current state: %d %s %s %s", currentVersion, record, variant, launch)
			}
		})
	}
	// A harmless custom section changes bytes while retaining a fully valid cartridge.
	changed := append(bytes.Clone(cart), 0, 2, 1, 'x')
	upload := completeUpload(t, ctx, database, uploader, "replacement.zip", replacementZIP(t, "cart.wasm", changed))
	job, err := service.Schedule(ctx, gameID, upload, version)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, ctx, database, job.JobID, "SUCCEEDED")
	var record, name string
	if err := dbapi.QueryRowContext(ctx, database, `SELECT file_record,logical_name FROM game_files WHERE game_id=?`,
		gameID).Scan(&record, &name); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(blobs.Path(record))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, changed) || name != "cart.wasm" {
		t.Fatal("replacement published archive instead of validated cartridge")
	}
}

func publishedWASMGame(t *testing.T) (dbapi.DB, *filestore.Store, *uploads.Service, string, []byte) {
	t.Helper()
	ctx := t.Context()
	directory := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close replacement database", database.Close()) })
	set, err := dependencies.Load("../../../data", []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(set, dependencypersistence.New(database.SQL)).Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := filestore.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	uploader := uploads.New(uploadpersistence.New(database.SQL), blobs, directory, time.Now)
	cart, err := os.ReadFile("../../../testdata/public-roms/wasm4-controls/controls.wasm")
	if err != nil {
		t.Fatal(err)
	}
	upload := completeUpload(t, ctx, database.SQL, uploader, "cart.wasm", cart)
	fixture := importfixture.New(t, database.SQL, blobs, importfixture.Options{Now: time.Now})
	created, err := fixture.Create(ctx, libraryimport.CreateRequest{
		UploadID:                 upload,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "wasm4/wasm4"), MetadataProvider: "NONE",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := dbapi.QueryRowContext(ctx, database.SQL, "SELECT id FROM import_items WHERE import_job_id=?", created.ImportJobID).
		Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	published, err := fixture.Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return database.SQL, blobs, uploader, published.GameID, cart
}

func replacementZIP(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
