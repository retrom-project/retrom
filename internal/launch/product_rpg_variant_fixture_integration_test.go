//go:build integration

package launch

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	dependencypersistence "retrom/internal/persistence/dependencies"
	savepersistence "retrom/internal/persistence/saves"
	uploadpersistence "retrom/internal/persistence/uploads"
	retromruntime "retrom/internal/runtime"
	dependencyservice "retrom/internal/service/dependencies"
	"retrom/internal/service/saves"
	"retrom/internal/service/uploads"
	"retrom/internal/testsupport"
)

type productRPGFixture struct {
	service  *Service
	database *sql.DB
	blobs    *blobstore.Store
	gameID   string
	now      func() time.Time
}

func newProductRPGFixture(t *testing.T, generation string) productRPGFixture {
	t.Helper()
	ctx, dataDir := t.Context(), t.TempDir()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close RPG variant fixture", database.Close()) })
	seedLocalProfile(t, database.SQL)
	mustRPGLaunchSQL(t, database.SQL, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES('rpg-product-admin','local','rpg-product-admin','RPG admin','ADMIN','ENABLED',0,0)`)
	dependencySet, err := dependencies.Load("../../data", []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(dependencySet, dependencypersistence.New(database.SQL)).Bootstrap(ctx, now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploadID := uploadProductRPGFixture(t, database.SQL, blobs, dataDir, generation, now)
	importer := libraryimport.New(database.SQL, now).WithBlobStore(blobs)
	created, err := importer.Create(ctx, libraryimport.CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "rpgmaker/rpgmaker"),
		MetadataProvider: "NONE", ContentMode: "RPG_MAKER_PROJECT",
	})
	if err != nil {
		t.Fatalf("import RPG fixture: %v", err)
	}
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `SELECT id FROM import_items WHERE import_job_id=?`, created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	title := "RPG variant " + generation
	patched, err := importer.PatchDraft(ctx, itemID, 1, libraryimport.DraftPatch{Metadata: &libraryimport.MetadataPatch{Title: &title}, TagIDs: []string{}})
	if err != nil {
		t.Fatalf("patch RPG fixture: %v", err)
	}
	approved, err := importer.Approve(ctx, itemID, patched.Version)
	if err != nil {
		t.Fatalf("approve RPG fixture: %v", err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, now).WithBlobStore(blobs).
		WithRPGRuntimeOriginTemplate("https://{launchId}.rpg-runtime.example").
		WithRuntimeProvider(dependencySet.RuntimeCatalog, builder)
	return productRPGFixture{service: service, database: database.SQL, blobs: blobs, gameID: approved.GameID, now: now}
}

func uploadProductRPGFixture(t *testing.T, database *sql.DB, blobs *blobstore.Store, dataDir, generation string, now func() time.Time) string {
	t.Helper()
	ctx := t.Context()
	archive := productRPGArchive(t, generation)
	uploader := uploads.New(uploadpersistence.New(database), blobs, dataDir, now)
	upload, err := uploader.Create(ctx, uploads.CreateRequest{
		Purpose: "PROJECT", SourceType: "FILES", Files: []uploads.FileDeclaration{{
			ClientFileID: "project", RelativePath: generation + ".zip", SizeBytes: int64(len(archive)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	err = uploader.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(archive)-1, len(archive)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	current, err := uploader.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := uploader.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForONSReviewJob(t, ctx, database, jobID)
	return upload.ID
}

func productRPGArchive(t *testing.T, generation string) []byte {
	t.Helper()
	root := filepath.Join("../../testdata/public-roms/rpgmaker-smoke", generation)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := writer.Create(filepath.ToSlash(name))
		if err != nil {
			return err
		}
		_, err = file.Write(contents)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func productRPGSavedLaunch(t *testing.T, fixture productRPGFixture, target string) (Created, string) {
	t.Helper()
	// Establish the valid original launch while only its detected Target is eligible.
	// Re-enabling all generation bindings then exercises the actual repeated selection bug.
	mustRPGLaunchSQL(t, fixture.database, `UPDATE runtime_target_bindings SET launch_policy='DISABLED' WHERE core_id='rpgmaker' AND target_id<>?`, target)
	original, err := fixture.service.Create(t.Context(), "local", CreateRequest{GameID: fixture.gameID, ReturnTo: "/games/" + fixture.gameID})
	if err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, fixture.database, `UPDATE runtime_target_bindings SET launch_policy='SUPPORTED' WHERE core_id='rpgmaker'`)
	if _, err := fixture.service.Config(t.Context(), original.LaunchID, original.Capability); err != nil {
		t.Fatal(err)
	}
	saver := saves.New(savepersistence.New(fixture.database), fixture.blobs, fixture.now)
	saved, _, err := saver.CreateManual(t.Context(), original.LaunchID, original.Capability, "rpg-variant-save", reviewCheckpointRequest(t, "opaque-rpg-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.SaveStateID == "" {
		t.Fatalf("save=%+v", saved)
	}
	return original, saved.SaveStateID
}
