//go:build integration

package launch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
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

func TestWASM4SingleCartReviewPublishesProductLaunch(t *testing.T) {
	if os.Getenv("RETROM_PFB_ID") == "" {
		t.Skip("WASM-4 is a PFB candidate until its first formal runtime release")
	}
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const actorID = "01980000-0000-7000-8000-000000009982"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('wasm4-profile','WASM-4 Admin',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'wasm4-profile','wasm4-admin','WASM-4 Admin','ADMIN','ENABLED',0,0);
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
	if _, _, exists := dependencySet.RetromRuntimeFile("v0.11.1", "wasm4-retrom.mjs"); !exists {
		t.Fatal("WASM-4 runtime module is absent from the PFB allowlist")
	}

	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	// The deterministic fallback exercises the HTTP/domain pipeline. An opt-in
	// acceptance run may point at one of the separately licensed real cartridges.
	cart := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if cartPath := os.Getenv("RETROM_WASM4_TEST_CART"); cartPath != "" {
		cart, err = os.ReadFile(cartPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES", Files: []uploads.FileDeclaration{{
			ClientFileID: "pong", RelativePath: "Pong.wasm", SizeBytes: int64(len(cart)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(cart)
	contentDigest := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := uploadService.PutPart(
		ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(cart)-1, len(cart)), contentDigest, bytes.NewReader(cart),
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
	waitForWASM4Job(t, database.SQL, jobID)

	importService := libraryimport.New(database.SQL, time.Now)
	createdImport, err := importService.Create(ctx, libraryimport.CreateRequest{
		UploadID:                 upload.ID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "wasm4/wasm4"),
		MetadataProvider:         "NONE",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.SQL.QueryRowContext(
		ctx, `SELECT id FROM import_items WHERE import_job_id=?`, createdImport.ImportJobID,
	).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs)
	preview, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: itemID, ActorUserID: actorID, IdempotencyKey: "wasm4-preview-1",
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil || !preview.CaptureAllowed {
		t.Fatalf("CreateReviewPreview(WASM-4)=%#v, %v", preview, err)
	}
	previewConfig, err := service.ReviewPreviewConfig(ctx, preview.PreviewID, preview.Capability)
	if err != nil || previewConfig.WASM4 == nil || previewConfig.WASM4.Purpose != "REVIEW_PREVIEW" ||
		previewConfig.WASM4.CartSizeBytes != int64(len(cart)) ||
		previewConfig.WASM4.Adapter.AdapterKind != "WASM4_WEB" {
		t.Fatalf("ReviewPreviewConfig(WASM-4)=%#v, %v", previewConfig, err)
	}
	previewContent, err := service.ReviewPreviewContent(
		ctx, preview.PreviewID, preview.Capability, "Pong.wasm",
	)
	if err != nil || previewContent.Digest != base64DigestHex(digest) || previewContent.Format != "SOURCE_V1" {
		t.Fatalf("ReviewPreviewContent(WASM-4)=%#v, %v", previewContent, err)
	}

	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StoreReviewScreenshot(
		ctx, preview.PreviewID, preview.Capability, bytes.NewReader(png),
	); err != nil {
		t.Fatal(err)
	}
	approved, err := importService.Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Create(ctx, "wasm4-profile", CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: Capabilities{SecureContext: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := service.Config(ctx, created.LaunchID, created.Capability)
	if err != nil || product.WASM4 == nil || product.WASM4.Purpose != "PRODUCT" ||
		product.WASM4.ContentDigest != base64DigestHex(digest) || product.WASM4.CartSizeBytes != int64(len(cart)) ||
		product.WASM4.Adapter.CartURL == "" || product.WASM4.Checkpoint != nil {
		t.Fatalf("Config(WASM-4 product)=%#v, %v", product, err)
	}
	if encoded, err := MarshalConfig(product); err != nil || !bytes.Contains(encoded, []byte(`"runtimeFamily":"WASM4"`)) {
		t.Fatalf("MarshalConfig(WASM-4)=%s, %v", encoded, err)
	}
	servedDigest, err := service.ContentBlob(ctx, created.LaunchID, created.Capability, "Pong.wasm")
	if err != nil || servedDigest != base64DigestHex(digest) {
		t.Fatalf("ContentBlob(WASM-4)=%q, %v", servedDigest, err)
	}
}

func waitForWASM4Job(t *testing.T, database *sql.DB, jobID string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.QueryRowContext(context.Background(), `SELECT state FROM jobs WHERE id=?`, jobID).
			Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("WASM-4 upload finalization=%s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
