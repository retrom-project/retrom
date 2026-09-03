//go:build integration

package launch

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	"retrom/internal/rpgmaker/isolation"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestTyranoScriptReviewPreviewPublishesProductLaunch(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const actorID = "01980000-0000-7000-8000-000000009984"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('tyrano-preview-profile','Tyrano Admin',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'tyrano-preview-profile','tyrano-preview-admin','Tyrano Admin','ADMIN','ENABLED',0,0);
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
	itemID, importService := createTyranoScriptReviewItem(t, ctx, database.SQL, blobs, dataDir)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	runtimeBuilder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs).
		WithRPGRuntimeOriginTemplate("https://{launchId}.rpg-runtime.example").
		WithRuntimeProvider(dependencySet.RuntimeCatalog, runtimeBuilder)
	preview, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: itemID, ActorUserID: actorID, IdempotencyKey: "tyrano-preview-1",
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil || !preview.CaptureAllowed {
		t.Fatalf("CreateReviewPreview(TyranoScript)=%#v, %v", preview, err)
	}
	configuration, err := service.ReviewPreviewConfig(ctx, preview.PreviewID, preview.Capability)
	if err != nil {
		t.Fatalf("ReviewPreviewConfig(TyranoScript)=%#v, %v", configuration, err)
	}
	previewEnvelope := testsupport.RuntimeEnvelope(t, configuration)
	previewSession := testsupport.RuntimeEnvelopeObject(t, previewEnvelope, "session")
	previewRuntime := testsupport.RuntimeEnvelopeObject(t, previewEnvelope, "runtime")
	previewGame := testsupport.RuntimeEnvelopeResource(t, previewEnvelope, "game")
	previewOrigin, _ := previewGame["origin"].(string)
	previewTicket, _ := previewGame["bootstrapTicket"].(string)
	if previewSession["purpose"] != "REVIEW_PREVIEW" || previewRuntime["targetId"] != "tyranoscript" ||
		previewTicket == "" || previewGame["entryUrl"] != previewOrigin+"/__retrom/tyranoscript/bootstrap" ||
		previewGame["cleanupUrl"] != previewOrigin+"/__retrom/tyranoscript/cleanup" {
		t.Fatalf("TyranoScript preview envelope=%#v", previewEnvelope)
	}
	if identity, err := service.ProjectContentIdentity(
		ctx, preview.PreviewID, preview.Capability,
	); err != nil || identity == "" {
		t.Fatalf("TyranoScript preview content identity=%q, %v", identity, err)
	}
	isolationService := isolation.New(
		database.SQL, "https://{launchId}.rpg-runtime.example", time.Now,
	)
	previewCredential, previewAccess, err := isolationService.ConsumeTicket(
		ctx, preview.PreviewID, previewOrigin, previewTicket,
	)
	if err != nil || !previewAccess.Preview || previewAccess.ContentFormat != tyranoScriptProjectFormat {
		t.Fatalf("consume TyranoScript preview ticket=%#v, %v", previewAccess, err)
	}
	if authorized, err := isolationService.Authenticate(
		ctx, preview.PreviewID, previewOrigin, previewCredential,
	); err != nil || !authorized.Preview || authorized.ContentFormat != tyranoScriptProjectFormat {
		t.Fatalf("authenticate TyranoScript preview=%#v, %v", authorized, err)
	}
	var lockedPreviewID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT preview_id FROM isolated_runtime_bootstrap_tickets WHERE preview_id=?
`, preview.PreviewID).Scan(&lockedPreviewID); err != nil || lockedPreviewID != preview.PreviewID {
		t.Fatalf("TyranoScript preview bootstrap ticket=%q, %v", lockedPreviewID, err)
	}
	for _, logicalName := range []string{"index.html", "data/scenario/first.ks", "tyrano/tyrano.js"} {
		content, contentErr := service.TyranoScriptProjectContentAuthorized(
			ctx, preview.PreviewID, logicalName, true,
		)
		if contentErr != nil || content.Format != tyranoScriptProjectFormat || content.Digest == "" {
			t.Fatalf("preview content %q=%#v, %v", logicalName, content, contentErr)
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 220, G: 70, B: 40, A: 255})
	var screenshot bytes.Buffer
	if err := jpeg.Encode(&screenshot, canvas, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StoreReviewScreenshot(
		ctx, preview.PreviewID, preview.Capability, bytes.NewReader(screenshot.Bytes()),
	); err != nil {
		t.Fatal(err)
	}
	approved, err := importService.Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatalf("Approve(TyranoScript)=%v", err)
	}
	created, err := service.Create(ctx, "tyrano-preview-profile", CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil {
		t.Fatalf("Create(TyranoScript product)=%v", err)
	}
	product, err := service.Config(ctx, created.LaunchID, created.Capability)
	if err != nil {
		t.Fatalf("Config(TyranoScript product)=%#v, %v", product, err)
	}
	productEnvelope := testsupport.RuntimeEnvelope(t, product)
	productSession := testsupport.RuntimeEnvelopeObject(t, productEnvelope, "session")
	productGame := testsupport.RuntimeEnvelopeResource(t, productEnvelope, "game")
	productOrigin, _ := productGame["origin"].(string)
	productTicket, _ := productGame["bootstrapTicket"].(string)
	if productSession["purpose"] != "PRODUCT" || productEnvelope["restore"] != nil || productTicket == "" {
		t.Fatalf("TyranoScript product envelope=%#v", productEnvelope)
	}
	productCredential, productAccess, err := isolationService.ConsumeTicket(
		ctx, created.LaunchID, productOrigin, productTicket,
	)
	if err != nil || productAccess.Preview || productAccess.ContentFormat != tyranoScriptProjectFormat {
		t.Fatalf("consume TyranoScript product ticket=%#v, %v", productAccess, err)
	}
	if authorized, err := isolationService.Authenticate(
		ctx, created.LaunchID, productOrigin, productCredential,
	); err != nil || authorized.Preview || authorized.ContentFormat != tyranoScriptProjectFormat {
		t.Fatalf("authenticate TyranoScript product=%#v, %v", authorized, err)
	}
	content, err := service.TyranoScriptProjectContentAuthorized(ctx, created.LaunchID, "index.html", false)
	if err != nil || content.Format != tyranoScriptProjectFormat {
		t.Fatalf("product content=%#v, %v", content, err)
	}
	assertTyranoScriptPreviewIsolationCleanup(t, database.SQL, preview.PreviewID)
}

func assertTyranoScriptPreviewIsolationCleanup(t *testing.T, database *sql.DB, previewID string) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE review_preview_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?
WHERE id=? AND state IN ('CREATED','ACTIVE')`, []any{now, now, previewID}},
		{`DELETE FROM isolated_runtime_capabilities WHERE preview_id=?`, []any{previewID}},
		{`DELETE FROM isolated_runtime_bootstrap_tickets WHERE preview_id=?`, []any{previewID}},
		{`DELETE FROM review_preview_files WHERE preview_session_id=?`, []any{previewID}},
		{`DELETE FROM review_runtime_screenshots WHERE preview_session_id=?`, []any{previewID}},
		{`DELETE FROM review_preview_sessions WHERE id=?`, []any{previewID}},
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("delete terminal TyranoScript preview: %v", err)
		}
	}
	var retained int
	if err := database.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM isolated_runtime_bootstrap_tickets WHERE preview_id=?)+
       (SELECT count(*) FROM isolated_runtime_capabilities WHERE preview_id=?)
`, previewID, previewID).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("retained TyranoScript preview credentials=%d, %v", retained, err)
	}
	rows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close foreign key check", rows.Close()) }()
	if rows.Next() {
		t.Fatal("TyranoScript preview cleanup left a foreign key violation")
	}
}

func createTyranoScriptReviewItem(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	dataDir string,
) (string, *libraryimport.Service) {
	t.Helper()
	archive := tyranoScriptReviewArchive(t)
	uploadService := uploads.New(database, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		Purpose: "TYRANOSCRIPT_PROJECT", SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "tyrano", RelativePath: "tyrano-review.zip", SizeBytes: int64(len(archive)),
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
		UploadID: upload.ID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(
			t, database, "tyranoscript/tyranoscript",
		),
		MetadataProvider: "NONE", ContentMode: tyranoScriptProjectFormat,
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.QueryRowContext(
		ctx, `SELECT id FROM import_items WHERE import_job_id=?`, created.ImportJobID,
	).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return itemID, importService
}

func tyranoScriptReviewArchive(t *testing.T) []byte {
	t.Helper()
	files := []struct{ name, contents string }{
		{"Fixture/index.html", "<!doctype html><html><head></head><body></body></html>"},
		{"Fixture/data/scenario/first.ks", "*start\n[cm]"},
		{"Fixture/data/system/Config.tjs", "; config"},
		{"Fixture/tyrano/plugins/kag/kag.js", "window.tyrano={};"},
		{"Fixture/tyrano/tyrano.js", "window.tyrano=window.tyrano||{};"},
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, file := range files {
		writer, err := archive.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(file.contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
