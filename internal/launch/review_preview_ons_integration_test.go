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
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestONSReviewPreviewRunsProjectAndUnlocksApproval(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const actorID = "01980000-0000-7000-8000-000000009994"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('ons-preview-profile','ONS Admin',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'ons-preview-profile','ons-preview-admin','ONS Admin','ADMIN','ENABLED',0,0);
`, actorID); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	itemID, importService := createONSReviewItem(t, ctx, database.SQL, blobs, dataDir)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs)
	preview, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: itemID, ActorUserID: actorID, IdempotencyKey: "ons-preview-1",
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil || !preview.CaptureAllowed || preview.CaptureAfterMS != 5_000 {
		t.Fatalf("CreateReviewPreview(ONS) = %#v, %v", preview, err)
	}
	configuration, err := service.ReviewPreviewConfig(ctx, preview.PreviewID, preview.Capability)
	if err != nil || configuration.ONS == nil {
		t.Fatalf("ReviewPreviewConfig(ONS) = %#v, %v", configuration, err)
	}
	onsConfig := configuration.ONS
	if onsConfig.RuntimeFamily != "ONS" || onsConfig.Purpose != "REVIEW_PREVIEW" ||
		onsConfig.Adapter.AdapterKind != "ONS_YURI_WEB" || onsConfig.Adapter.AdapterID != "ons-yuri-web" ||
		onsConfig.Adapter.ScriptEncoding != "utf8" || onsConfig.Adapter.CheckpointSlot != 999 ||
		onsConfig.Adapter.ProjectIndexURL != "/runtime/projects/"+preview.PreviewID+"/index.json" {
		t.Fatalf("ONS review config = %#v", onsConfig)
	}
	encoded, err := json.Marshal(configuration)
	if err != nil || !bytes.Contains(encoded, []byte(`"checkpoint":null`)) ||
		bytes.Contains(encoded, []byte(`"emulatorjsVersion"`)) {
		t.Fatalf("ONS review config JSON = %s, %v", encoded, err)
	}
	index, err := service.ReviewPreviewProjectIndex(ctx, preview.PreviewID, preview.Capability)
	if err != nil {
		t.Fatal(err)
	}
	var projectIndex struct {
		Files []struct {
			Path string `json:"path"`
			URL  string `json:"url"`
		} `json:"files"`
		FontPath string `json:"fontPath"`
	}
	if json.Unmarshal(index.Contents, &projectIndex) != nil || projectIndex.FontPath != "default.ttf" ||
		len(projectIndex.Files) != 4 {
		t.Fatalf("ONS project index = %s", index.Contents)
	}
	for _, file := range projectIndex.Files {
		content, contentErr := service.ReviewPreviewProjectContent(
			ctx, preview.PreviewID, preview.Capability, file.Path,
		)
		if contentErr != nil || content.Format != onsProjectFormat || content.Digest == "" ||
			file.URL != "/runtime/projects/"+preview.PreviewID+"/"+file.Path {
			t.Fatalf("ONS project file %q = %#v, %v", file.Path, content, contentErr)
		}
	}
	pngBody, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StoreReviewScreenshot(
		ctx, preview.PreviewID, preview.Capability, bytes.NewReader(pngBody),
	); err != nil {
		t.Fatal(err)
	}
	approved, err := importService.Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatalf("Approve(ONS) error = %v", err)
	}
	var contentKind, compatibilityCode string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT content.content_kind,revision.compatibility_code
FROM games game
JOIN game_content_revisions content ON content.id=game.current_content_revision_id
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
WHERE game.id=?
`, approved.GameID).Scan(&contentKind, &compatibilityCode); err != nil ||
		contentKind != onsProjectFormat || compatibilityCode != reviewScreenshotOverrideCode {
		t.Fatalf("published ONS = %s/%s, %v", contentKind, compatibilityCode, err)
	}
}

func createONSReviewItem(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	dataDir string,
) (string, *libraryimport.Service) {
	t.Helper()
	archive := onsReviewArchive(t)
	uploadService := uploads.New(database, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		Purpose: "ONS_PROJECT", SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "ons", RelativePath: "ons-review.zip", SizeBytes: int64(len(archive)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	if err := uploadService.PutPart(
		ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(archive)-1, len(archive)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(archive),
	); err != nil {
		t.Fatal(err)
	}
	current, err := uploadService.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForONSReviewJob(t, ctx, database, jobID)
	importService := libraryimport.New(database, time.Now).WithBlobStore(blobs)
	created, err := importService.Create(ctx, libraryimport.CreateRequest{
		UploadID:                 upload.ID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database, "ons/onscripter_yuri"),
		MetadataProvider:         "NONE", ContentMode: onsProjectFormat,
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.QueryRowContext(ctx, `SELECT id FROM import_items WHERE import_job_id=?`, created.ImportJobID).
		Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return itemID, importService
}

func waitForONSReviewJob(t *testing.T, ctx context.Context, database *sql.DB, jobID string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("ONS upload finalization = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func onsReviewArchive(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range []struct {
		name, body string
	}{
		{"Fixture/0.txt", "*define\n*start\n"},
		{"Fixture/default.ttf", "fixture-font"},
		{"Fixture/arc.nsa", "opaque ONS archive"},
		{"Fixture/bg/scene.png", "fixture-image"},
	} {
		entry, err := writer.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
