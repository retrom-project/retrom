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
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	retromsaves "retrom/internal/saves"
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
	runtimeBuilder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs).
		WithRuntimeProvider(dependencySet.RuntimeCatalog, runtimeBuilder)
	preview, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: itemID, ActorUserID: actorID, IdempotencyKey: "ons-preview-1",
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil {
		t.Fatalf("CreateReviewPreview(ONS) = %#v, %v", preview, err)
	}
	configuration, err := service.ReviewPreviewConfig(ctx, preview.PreviewID, preview.Capability)
	if err != nil {
		t.Fatalf("ReviewPreviewConfig(ONS) = %#v, %v", configuration, err)
	}
	envelope := testsupport.RuntimeEnvelope(t, configuration)
	session := testsupport.RuntimeEnvelopeObject(t, envelope, "session")
	runtimeIdentity := testsupport.RuntimeEnvelopeObject(t, envelope, "runtime")
	options := testsupport.RuntimeEnvelopeObject(t, envelope, "targetOptions")
	gameResource := testsupport.RuntimeEnvelopeResource(t, envelope, "game")
	projectIdentity, identityErr := service.ProjectContentIdentity(
		ctx, preview.PreviewID, preview.Capability,
	)
	projectRoot, rootErr := RuntimeProjectContentRoot(projectIdentity)
	if session["purpose"] != "REVIEW_PREVIEW" || runtimeIdentity["targetId"] != "onscripter-yuri" ||
		options["scriptEncoding"] != "utf8" || identityErr != nil || rootErr != nil ||
		gameResource["indexUrl"] != projectRoot+"index.json" || envelope["restore"] != nil {
		t.Fatalf("ONS review envelope = %#v", envelope)
	}
	encoded, err := json.Marshal(configuration)
	if err != nil || !bytes.Contains(encoded, []byte(`"restore":null`)) ||
		bytes.Contains(encoded, []byte(`"emulatorjsVersion"`)) {
		t.Fatalf("ONS review config JSON = %s, %v", encoded, err)
	}
	index, err := service.ReviewPreviewProjectIndex(ctx, preview.PreviewID, preview.Capability)
	if err != nil {
		t.Fatal(err)
	}
	var projectIndex struct {
		Files []struct {
			Path      string `json:"path"`
			SizeBytes int64  `json:"sizeBytes"`
			URL       string `json:"url"`
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
			file.SizeBytes < 1 ||
			file.URL != projectRoot+file.Path {
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
SELECT game.content_kind,variant.compatibility_code
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
WHERE game.id=?
`, approved.GameID).Scan(&contentKind, &compatibilityCode); err != nil ||
		contentKind != onsProjectFormat || compatibilityCode != reviewScreenshotOverrideCode {
		t.Fatalf("published ONS = %s/%s, %v", contentKind, compatibilityCode, err)
	}
	assertONSProductRoundTrip(
		t, ctx, service, database.SQL, blobs, approved.GameID, pngBody,
	)
}

func assertONSProductRoundTrip(
	t *testing.T,
	ctx context.Context,
	service *Service,
	database *sql.DB,
	blobs *blobstore.Store,
	gameID string,
	screenshot []byte,
) {
	t.Helper()
	created, err := service.Create(ctx, "ons-preview-profile", CreateRequest{
		GameID: gameID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil {
		t.Fatalf("Create(ONS product) error = %v", err)
	}
	config, err := service.Config(ctx, created.LaunchID, created.Capability)
	if err != nil {
		t.Fatalf("Config(ONS product) = %#v, %v", config, err)
	}
	productEnvelope := testsupport.RuntimeEnvelope(t, config)
	productSession := testsupport.RuntimeEnvelopeObject(t, productEnvelope, "session")
	productGame := testsupport.RuntimeEnvelopeResource(t, productEnvelope, "game")
	if productSession["purpose"] != "PRODUCT" || productEnvelope["restore"] != nil {
		t.Fatalf("ONS product envelope = %#v", productEnvelope)
	}
	index, err := service.ProjectIndex(ctx, created.LaunchID, created.Capability)
	if err != nil || !bytes.Contains(index.Contents, []byte(`"path":"0.txt"`)) ||
		!bytes.Contains(index.Contents, []byte(`"path":"default.ttf"`)) ||
		!bytes.Contains(index.Contents, []byte(`"sizeBytes":`)) {
		t.Fatalf("ProjectIndex(ONS product) = %s, %v", index.Contents, err)
	}
	content, err := service.Content(ctx, created.LaunchID, created.Capability, "0.txt")
	if err != nil || content.Format != onsProjectFormat {
		t.Fatalf("Content(ONS product) = %#v, %v", content, err)
	}
	saveService := retromsaves.New(database, blobs, service.credentials, time.Now)
	checkpoint := []byte("RETROM ONS CHECKPOINT V1")
	result, replayed, err := saveService.CreateManual(
		ctx, created.LaunchID, created.Capability, "ons-product-save-1",
		onsManualRequest(t, checkpoint, screenshot),
	)
	if err != nil || replayed || result.ResourceKind != "SAVE_STATE" || result.SaveStateID == "" ||
		result.CheckpointFormat != "test-checkpoint-v1" {
		t.Fatalf("CreateManual(ONS) = %#v, replayed=%v, err=%v", result, replayed, err)
	}
	var providerID, targetID, checkpointFormat string
	if err := database.QueryRowContext(ctx, `
SELECT launch.provider_id,launch.target_id,save.checkpoint_format
FROM launch_sessions launch
JOIN save_states save ON save.source_launch_session_id=launch.id
WHERE launch.id=? AND save.id=?
`, created.LaunchID, result.SaveStateID).Scan(&providerID, &targetID, &checkpointFormat); err != nil ||
		providerID != "retrom-runtime" || targetID != "onscripter-yuri" || checkpointFormat != "test-checkpoint-v1" {
		t.Fatalf("original ONS save binding = %s/%s/%s, error=%v", providerID, targetID, checkpointFormat, err)
	}
	restored, err := service.Create(ctx, "ons-preview-profile", CreateRequest{
		GameID: gameID, SaveStateID: &result.SaveStateID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil || restored.LaunchID == created.LaunchID {
		t.Fatalf("Create(ONS restore) = %#v, %v", restored, err)
	}
	restoreConfig, err := service.Config(ctx, restored.LaunchID, restored.Capability)
	if err != nil {
		t.Fatalf("Config(ONS restore) = %#v, %v", restoreConfig, err)
	}
	restoreEnvelope := testsupport.RuntimeEnvelope(t, restoreConfig)
	restore := testsupport.RuntimeEnvelopeObject(t, restoreEnvelope, "restore")
	restoreGame := testsupport.RuntimeEnvelopeResource(t, restoreEnvelope, "game")
	if restore["format"] != "test-checkpoint-v1" ||
		restore["url"] != "/runtime/launches/"+restored.LaunchID+"/state" ||
		restoreGame["indexUrl"] != productGame["indexUrl"] {
		t.Fatalf("ONS restore envelope = %#v", restoreEnvelope)
	}
	digest, err := saveService.StateDigest(ctx, restored.LaunchID, restored.Capability)
	expected := sha256.Sum256(checkpoint)
	if err != nil || digest != fmt.Sprintf("%x", expected) {
		t.Fatalf("StateDigest(ONS restore) = %s, %v", digest, err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE runtime_targets SET checkpoint_json='{"writeFormat":"replacement-v2","readFormats":["replacement-v2"],"maxBytes":268435456}'
WHERE provider_id=? AND target_id=?
`, providerID, targetID); err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(ctx, "ons-preview-profile", CreateRequest{
		GameID: gameID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil {
		t.Fatalf("Create(ONS after incompatible save upgrade) error = %v", err)
	}
	var launchCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM launch_sessions`).Scan(&launchCount); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, "ons-preview-profile", CreateRequest{
		GameID: gameID, SaveStateID: &result.SaveStateID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: Capabilities{SecureContext: true},
	}); !errors.Is(err, ErrSaveIncompatible) {
		t.Fatalf("Create(ONS incompatible restore) error = %v, want %v", err, ErrSaveIncompatible)
	}
	var launchCountAfter int
	var compatibilityStatus string
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM launch_sessions`).Scan(&launchCountAfter); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
SELECT status FROM save_state_runtime_compatibility WHERE save_state_id=?
`, result.SaveStateID).Scan(&compatibilityStatus); err != nil || launchCountAfter != launchCount ||
		compatibilityStatus != "INCOMPATIBLE_RUNTIME" {
		t.Fatalf("incompatible ONS save = launches:%d/%d status:%s error=%v",
			launchCount, launchCountAfter, compatibilityStatus, err)
	}
}

func onsManualRequest(t *testing.T, checkpoint, screenshot []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadataHeader := make(textproto.MIMEHeader)
	metadataHeader.Set("Content-Disposition", `form-data; name="metadata"; filename="metadata.json"`)
	metadataHeader.Set("Content-Type", "application/json")
	metadata, err := writer.CreatePart(metadataHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = metadata.Write([]byte(`{"checkpointFormat":"test-checkpoint-v1","name":"ONS 手动存档"}`))
	payload, err := writer.CreateFormFile("payload", "checkpoint.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = payload.Write(checkpoint)
	screenshotHeader := make(textproto.MIMEHeader)
	screenshotHeader.Set("Content-Disposition", `form-data; name="screenshot"; filename="screenshot.png"`)
	screenshotHeader.Set("Content-Type", "image/png")
	screenshotPart, err := writer.CreatePart(screenshotHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = screenshotPart.Write(screenshot)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
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
		Purpose: "PROJECT", SourceType: "FILES",
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
