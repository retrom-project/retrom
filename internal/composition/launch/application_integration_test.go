//go:build integration

package launch_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	variantcomposition "retrom/internal/composition/gamevariant"
	gamevariant "retrom/internal/service/gamevariant"

	composition "retrom/internal/composition/launch"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
	"retrom/internal/testsupport"
)

func TestAssemblyServesRealPreviewAndProduct(t *testing.T) {
	fixture := newAssemblyFixture(t)
	preview, err := fixture.previews.Create(t.Context(), application.ReviewPreviewRequest{ImportItemID: fixture.itemID, ActorUserID: assemblyActor, IdempotencyKey: "assembly-preview"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.ReviewPreviewConfig(t.Context(), preview.PreviewID, preview.Capability); err != nil {
		t.Fatal(err)
	}
	content, err := fixture.service.ReviewPreviewContent(t.Context(), preview.PreviewID, preview.Capability, "Assembly.nes")
	if err != nil || content.Digest == "" {
		t.Fatalf("preview content: %v", err)
	}
	approved, err := fixture.importer.Approve(t.Context(), fixture.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	created, err := fixture.service.Create(t.Context(), assemblyProfile, application.CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Config(t.Context(), created.LaunchID, created.Capability); err != nil {
		t.Fatal(err)
	}
	product, err := fixture.service.Content(t.Context(), created.LaunchID, created.Capability, "Assembly.nes")
	if err != nil || product.Digest != content.Digest {
		t.Fatalf("product content: %v", err)
	}
	if _, err := fixture.service.Content(t.Context(), created.LaunchID, "invalid", "Assembly.nes"); !errors.Is(err, application.ErrCredential) {
		t.Fatalf("invalid capability: %v", err)
	}
}

func TestAssemblyProductDispatchSharesCloseLifetimeAfterReceipt(t *testing.T) {
	fixture := newAssemblyFixture(t)
	approved, err := fixture.importer.Approve(t.Context(), fixture.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	entered, ended := make(chan struct{}), make(chan error, 1)
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{BeforeQuery: func(ctx context.Context, query string, args []driver.NamedValue) error {
		if strings.Contains(query, "FROM games game JOIN platform_instances") && len(args) == 2 && args[1].Value == approved.GameID {
			close(entered)
			<-ctx.Done()
			ended <- context.Cause(ctx)
			return context.Cause(ctx)
		}
		return nil
	}})
	variants := variantcomposition.New(fault, fixture.source, fixture.now, nil)
	service := composition.New(fault, fixture.source, "http://localhost:3000", fixture.now, variants.Dispatch)
	t.Cleanup(variants.Close)
	core := "nestopia"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	receipt, err := service.CreateProduct(ctx, application.ProductCreateCommand{ProfileID: assemblyProfile, ActorID: assemblyActor, Key: "assembly-worker", Digest: strings.Repeat("a", 64), Request: application.CreateRequest{GameID: approved.GameID, CoreID: &core, ReturnTo: "/games/" + approved.GameID}})
	if err != nil || receipt.Status != 202 || receipt.Created.JobID == "" {
		t.Fatalf("queued product: %+v %v", receipt, err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("composition worker did not reach facts")
	}
	var receipts int
	if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT count(*) FROM idempotency_records WHERE principal_id=? AND key=?`, assemblyActor, "assembly-worker").Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("dispatch preceded receipt: %d %v", receipts, err)
	}
	cancel()
	variants.Close()
	if !errors.Is(<-ended, gamevariant.ErrValidationWorkerClosed) {
		t.Fatal("background worker escaped process lifetime")
	}
	var state string
	var attempts int
	if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT state,attempt_count FROM jobs WHERE id=?`, receipt.Created.JobID).Scan(&state, &attempts); err != nil || state != "FAILED" || attempts != 1 {
		t.Fatalf("Close settlement %s/%d: %v", state, attempts, err)
	}
}
