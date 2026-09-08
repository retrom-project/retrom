//go:build integration

package launch

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"retrom/internal/libraryimport"
	"retrom/internal/testsupport"
)

func TestScummVMReviewSelectionPublishesExactGameAndCompleteTree(t *testing.T) {
	fixture := newScummVMFixture(t, []string{"One", "Two"})
	ctx := t.Context()
	firstID, snapshot := fixture.snapshot(t)
	if status, code := snapshot.Status(); status != "BLOCKED" || code != "SCUMMVM_ROOT_SELECTION_REQUIRED" {
		t.Fatalf("initial=%s/%s", status, code)
	}
	request := ReviewPreviewRequest{ImportItemID: fixture.itemID, ActorUserID: scummVMActor, IdempotencyKey: "scummvm-preview-1", ClientCapabilities: Capabilities{SecureContext: true}}
	if _, err := fixture.service.CreateReviewPreview(ctx, request); err == nil {
		t.Fatal("ambiguous game launched")
	}
	if _, err := fixture.importer.Approve(ctx, fixture.itemID, 1); err == nil {
		t.Fatal("ambiguous game approved")
	}
	foreign := strings.Repeat("f", 64)
	if _, err := fixture.importer.PatchDraft(ctx, fixture.itemID, 1, libraryimport.DraftPatch{TagIDs: []string{}, ScummVMCandidateID: &foreign}); err == nil {
		t.Fatal("foreign candidate selected")
	}
	selected := snapshot.Detection.Candidates[1]
	version, err := fixture.importer.PatchDraft(ctx, fixture.itemID, 1, libraryimport.DraftPatch{TagIDs: []string{}, ScummVMCandidateID: &selected.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.importer.PatchDraft(ctx, fixture.itemID, 1, libraryimport.DraftPatch{TagIDs: []string{}, ScummVMCandidateID: &selected.ID}); err == nil {
		t.Fatal("stale draft accepted")
	}
	nextID, next := fixture.snapshot(t)
	if nextID == firstID || next.SelectedCandidateID != selected.ID {
		t.Fatalf("selection not immutable: %s %s %+v", firstID, nextID, next)
	}
	request.IdempotencyKey = "scummvm-preview-2"
	preview, err := fixture.service.CreateReviewPreview(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	config, err := fixture.service.ReviewPreviewConfig(ctx, preview.PreviewID, preview.Capability)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testsupport.RuntimeEnvelope(t, config)
	options := testsupport.RuntimeEnvelopeObject(t, envelope, "targetOptions")
	if options["root"] != "Two" || options["engineId"] != "sky" || options["extra"] != "Floppy" {
		t.Fatalf("options=%+v", options)
	}
	assertScummVMTree(t, fixture.service, preview.PreviewID, preview.Capability)
	approved, err := fixture.importer.Approve(ctx, fixture.itemID, version.Version)
	if err != nil {
		t.Fatal(err)
	}
	launch, err := fixture.service.Create(ctx, "scummvm-profile", CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: Capabilities{SecureContext: true}})
	if err != nil {
		t.Fatal(err)
	}
	product, err := fixture.service.Config(ctx, launch.LaunchID, launch.Capability)
	if err != nil {
		t.Fatal(err)
	}
	options = testsupport.RuntimeEnvelopeObject(t, testsupport.RuntimeEnvelope(t, product), "targetOptions")
	if options["root"] != "Two" {
		t.Fatalf("product lost selected root: %+v", options)
	}
	assertScummVMTree(t, fixture.service, launch.LaunchID, launch.Capability)
}

func assertScummVMTree(t *testing.T, service *Service, id, capability string) {
	t.Helper()
	index, err := service.ProjectIndex(t.Context(), id, capability)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"One/game.bin", "Two/game.bin", "readme.txt"} {
		if !bytes.Contains(index.Contents, []byte(name)) {
			t.Fatalf("index omitted %s: %s", name, index.Contents)
		}
	}
	if _, err := service.ProjectIndex(t.Context(), id, "wrong-capability"); !errors.Is(err, ErrCredential) {
		t.Fatalf("unauthorized index=%v", err)
	}
}

func TestScummVMRevalidationPreservesSelectedNativeGame(t *testing.T) {
	fixture := newScummVMFixture(t, []string{"Two"})
	ctx := t.Context()
	approved, err := fixture.importer.Approve(ctx, fixture.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err := fixture.database.QueryRowContext(ctx, `SELECT dependency_snapshot_json FROM game_variants WHERE game_id=?`, approved.GameID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	request := CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: Capabilities{SecureContext: true}}
	pending, err := fixture.service.ensureVariant(ctx, "scummvm-profile", request, "", false)
	if err != nil || pending.JobID == "" {
		t.Fatalf("revalidation=%+v %v", pending, err)
	}
	fixture.service.ResumeValidationJob(ctx, pending.JobID)
	var after, status string
	if err := fixture.database.QueryRowContext(ctx, `SELECT dependency_snapshot_json,status FROM game_variants WHERE game_id=?`, approved.GameID).Scan(&after, &status); err != nil {
		t.Fatal(err)
	}
	if before != after || status != "READY" {
		t.Fatalf("revalidation lost native selection: status=%s before=%s after=%s", status, before, after)
	}
	if _, err := fixture.service.Create(ctx, "scummvm-profile", request); err != nil {
		t.Fatal(err)
	}
}
