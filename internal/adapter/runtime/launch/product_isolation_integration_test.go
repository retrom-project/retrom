//go:build integration

package launch

import (
	"bytes"
	"image"
	"image/jpeg"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/runtime/dependencies"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/foundation/cleanup"
	launchmodel "retrom/internal/model/launch"
	dependencypersistence "retrom/internal/repo/dependencies"
	dependencyservice "retrom/internal/service/dependencies"
	"retrom/internal/testkit/testsupport"
)

func TestProductIsolationCreationRollsBackTicketFilesAndReceipt(t *testing.T) {
	service, command := productIsolationFixture(t)
	assertProductCreationRollback(t, service, command)
	created, err := service.CreateProduct(t.Context(), command)
	if err != nil || created.Status != 201 || created.Created.LaunchID == "" {
		t.Fatalf("isolated create status=%d launch=%q error=%v", created.Status, created.Created.LaunchID, err)
	}
	var tickets, files, receipts int
	err = service.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM isolated_runtime_bootstrap_tickets WHERE launch_id=?),(SELECT count(*) FROM launch_content_files WHERE launch_session_id=?),(SELECT count(*) FROM idempotency_records WHERE principal_id=? AND key=?)`, created.Created.LaunchID, created.Created.LaunchID, command.ActorID, command.Key).Scan(&tickets, &files, &receipts)
	if err != nil || tickets != 1 || files == 0 || receipts != 1 {
		t.Fatalf("isolated records tickets=%d files=%d receipts=%d error=%v", tickets, files, receipts, err)
	}
}

func productIsolationFixture(t *testing.T) (*Service, launchmodel.ProductCreateCommand) {
	t.Helper()
	ctx := t.Context()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close product isolation fixture", database.Close()) })
	const actorID = "01980000-0000-7000-8000-000000009984"
	if _, err := database.SQL.ExecContext(ctx, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('tyrano-profile','Tyrano Admin',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'tyrano-profile','tyrano-admin','Tyrano Admin','ADMIN','ENABLED',0,0)`, actorID); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(root, "data"), []string{"4.2.3"}, "4.2.3")
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
	itemID, importer := createTyranoScriptReviewItem(t, ctx, database.SQL, blobs, dataDir, now)
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
	approveProductIsolationPreview(t, service, itemID, actorID)
	approved, err := importer.Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	command := launchmodel.ProductCreateCommand{
		ActorID: actorID, ProfileID: "tyrano-profile", Key: "isolated-product-rollback", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Request: CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: Capabilities{SecureContext: true}},
	}
	return service, command
}

func approveProductIsolationPreview(t *testing.T, service *Service, itemID, actorID string) {
	t.Helper()
	preview, err := service.CreateReviewPreview(t.Context(), ReviewPreviewRequest{ImportItemID: itemID, ActorUserID: actorID, IdempotencyKey: "product-isolation-fixture", ClientCapabilities: Capabilities{SecureContext: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewPreviewConfig(t.Context(), preview.PreviewID, preview.Capability); err != nil {
		t.Fatal(err)
	}
	var screenshot bytes.Buffer
	if err := jpeg.Encode(&screenshot, image.NewRGBA(image.Rect(0, 0, 2, 2)), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StoreReviewScreenshot(t.Context(), preview.PreviewID, preview.Capability, bytes.NewReader(screenshot.Bytes())); err != nil {
		t.Fatal(err)
	}
}
