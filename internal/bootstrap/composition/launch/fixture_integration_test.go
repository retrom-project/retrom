//go:build integration

package launch_test

import (
	"bytes"
	"context"
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
	"retrom/internal/adapter/runtime/launch"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	composition "retrom/internal/bootstrap/composition/launch"
	"retrom/internal/foundation/cleanup"
	uploadsmodel "retrom/internal/model/uploads"
	dependencypersistence "retrom/internal/repo/dependencies"
	uploadpersistence "retrom/internal/repo/uploads"
	dependencyservice "retrom/internal/service/dependencies"
	application "retrom/internal/service/launch"
	uploadsservice "retrom/internal/service/uploads"
	"retrom/internal/testkit/testsupport"
)

const (
	assemblyActor   = "01980000-0000-7000-8100-000000001070"
	assemblyProfile = "01980000-0000-7000-8100-000000001071"
)

type assemblyFixture struct {
	database *sql.DB
	source   *launch.Sources
	service  *application.Service
	importer *libraryimport.Service
	itemID   string
	now      func() time.Time
}

func newAssemblyFixture(t *testing.T) assemblyFixture {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	dir := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(dir, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close Launch assembly", database.Close()) })
	if _, err := database.SQL.ExecContext(t.Context(),
		`INSERT INTO profiles(id,display_name,created_at_ms)VALUES(?,'Assembly',?)`,
		assemblyProfile, now().UnixMilli()); err != nil {
		t.Fatalf("seed assembly profile: %v", err)
	}
	if _, err := database.SQL.ExecContext(t.Context(),
		`INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,'assembly','Assembly','ADMIN','ENABLED',?,?)`,
		assemblyActor, assemblyProfile, now().UnixMilli(), now().UnixMilli()); err != nil {
		t.Fatalf("seed assembly actor: %v", err)
	}
	deps, err := dependencies.Load(filepath.Join("..", "..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(deps, dependencypersistence.New(database.SQL)).Bootstrap(t.Context(), now()); err != nil {
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
	source := launch.NewSources(blobs, credentials).WithRuntimeProvider(builder)
	service := composition.New(database.SQL, source, "http://localhost:3000", now)
	t.Cleanup(service.Close)
	importer := libraryimport.New(database.SQL, now).WithBlobStore(blobs)
	itemID := uploadAssemblyROM(t, database.SQL, blobs, dir, importer, now)
	return assemblyFixture{database.SQL, source, service, importer, itemID, now}
}

func uploadAssemblyROM(t *testing.T, database *sql.DB, blobs *blobstore.Store, dir string, importer *libraryimport.Service, now func() time.Time) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "public-roms", "nes-smoke", "nes-smoke.nes"))
	if err != nil {
		t.Fatal(err)
	}
	uploader := uploadsservice.New(uploadpersistence.New(database), blobs, dir, now)
	upload, err := uploader.Create(t.Context(), uploadsmodel.CreateRequest{SourceType: "FILES", Files: []uploadsmodel.FileDeclaration{{ClientFileID: "game", RelativePath: "Assembly.nes", SizeBytes: int64(len(contents))}}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := uploader.PutPart(t.Context(), upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, err := uploader.Get(t.Context(), upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := uploader.Complete(t.Context(), upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitAssemblyUpload(t, database, job)
	imported, err := importer.Create(t.Context(), libraryimport.CreateRequest{UploadID: upload.ID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database, "nes/fceumm"), MetadataProvider: "NONE"})
	if err != nil {
		t.Fatal(err)
	}
	var item string
	if err := database.QueryRowContext(t.Context(), `SELECT id FROM import_items WHERE import_job_id=?`, imported.ImportJobID).Scan(&item); err != nil {
		t.Fatal(err)
	}
	return item
}

func waitAssemblyUpload(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		if err := database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, id).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		if state == "FAILED" {
			t.Fatal("assembly upload failed")
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
