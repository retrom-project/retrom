//go:build integration

package launch

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/scummvm"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

const scummVMActor = "01980000-0000-7000-8000-000000009975"

type scummVMFixture struct {
	service  *Service
	importer *libraryimport.Service
	database *sql.DB
	itemID   string
}

func newScummVMFixture(t *testing.T, roots []string) scummVMFixture {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.ExecContext(ctx, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('scummvm-profile','ScummVM Admin',0);
 INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms) VALUES(?,'scummvm-profile','scummvm-admin','ScummVM Admin','ADMIN','ENABLED',0,0)`, scummVMActor); err != nil {
		t.Fatal(err)
	}
	dependencySet, err := dependencies.Load(filepath.Join("..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	importer := libraryimport.New(database.SQL, time.Now).WithBlobStore(blobs).WithScummVMDetector(scummVMFixtureDetector(t, roots))
	itemID := uploadScummVMFixture(t, database.SQL, blobs, dir, importer)
	credentials, err := retromruntime.LoadOrCreateCredentials(dir)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs).WithRuntimeProvider(dependencySet.RuntimeCatalog, builder)
	return scummVMFixture{service, importer, database.SQL, itemID}
}

func scummVMFixtureDetector(t *testing.T, roots []string) *scummvm.Detector {
	t.Helper()
	const commit = "fed42f2068dcafc6aafa1c28c77e4c88def74b66"
	games := make([]scummvm.DetectedGame, 0, len(roots))
	for _, root := range roots {
		games = append(games, scummvm.DetectedGame{Root: root, EngineID: "sky", GameID: "sky", Description: "Fixture game", PreferredTarget: "sky", Language: "en", Platform: "pc", Extra: "Floppy", Config: map[string]string{}, CanBeAdded: true})
	}
	raw, err := json.Marshal(map[string]any{"schemaVersion": 1, "upstreamCommit": commit, "error": nil, "candidates": games})
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(t.TempDir(), "detector")
	script := "#!/bin/sh\nprintf '%s' '" + strings.ReplaceAll(string(raw), "'", "'\\''") + "'\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return scummvm.New(func(context.Context) (scummvm.Tool, error) {
		return scummvm.Tool{Path: tool, UpstreamCommit: commit, Engines: []string{"sky"}}, nil
	})
}

func uploadScummVMFixture(t *testing.T, database *sql.DB, blobs *blobstore.Store, dir string, importer *libraryimport.Service) string {
	t.Helper()
	ctx := t.Context()
	var body bytes.Buffer
	archive := zip.NewWriter(&body)
	for _, name := range []string{"One/game.bin", "Two/game.bin", "readme.txt"} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("Retrom own opaque test data: " + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	contents := body.Bytes()
	service := uploads.New(database, blobs, dir, time.Now)
	upload, err := service.Create(ctx, uploads.CreateRequest{Purpose: "PROJECT", SourceType: "FILES", Files: []uploads.FileDeclaration{{ClientFileID: "game", RelativePath: "scummvm.zip", SizeBytes: int64(len(contents))}}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := service.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForONSReviewJob(t, ctx, database, jobID)
	created, err := importer.Create(ctx, libraryimport.CreateRequest{UploadID: upload.ID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database, "scummvm/scummvm"), MetadataProvider: "NONE", ContentMode: scummvm.ContentKind})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.QueryRowContext(ctx, `SELECT id FROM import_items WHERE import_job_id=?`, created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return itemID
}

func (fixture scummVMFixture) snapshot(t *testing.T) (string, scummvm.Snapshot) {
	t.Helper()
	var id, raw string
	err := fixture.database.QueryRowContext(t.Context(), `SELECT validation.id,validation.dependency_snapshot_json FROM import_item_core_validations validation WHERE validation.import_item_id=? ORDER BY validation.created_at_ms DESC,validation.id DESC LIMIT 1`, fixture.itemID).Scan(&id, &raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := scummvm.ParseSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id, snapshot
}
