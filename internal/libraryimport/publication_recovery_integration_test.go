//go:build integration

package libraryimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
)

func TestPublicationRecoveryBeforeDirectoryMove(t *testing.T) {
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	itemID := created.Items[0].ItemID
	finishOwnedReviewHandoff(t, fixture, request)
	ctx := prepareApprovalSelections(t, fixture, itemID)
	var source string
	if err := dbapi.QueryRowContext(ctx, fixture.database, `SELECT file_record FROM import_item_source_snapshot_files WHERE source_snapshot_id=(SELECT
effective_source_snapshot_id FROM import_items WHERE id=?) LIMIT 1`, itemID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	record, err := filestore.ParseRecord(source)
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSuffix(fixture.blobs.Path(source), filepath.FromSlash(record.Path))
	obstruction := filepath.Join(root, "files")
	if err := os.WriteFile(obstruction, []byte("directory unavailable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Approve(ctx, itemID, 1); err == nil {
		t.Fatal("expected failed directory move")
	}
	var state, gameID string
	var games int
	if err := dbapi.QueryRowContext(ctx, fixture.database, `SELECT state,publication_game_id,(SELECT count(*) FROM games) FROM import_items WHERE id=?`, itemID).Scan(&state, &gameID, &games); err != nil {
		t.Fatal(err)
	}
	if state != "PUBLISHING" || gameID == "" || games != 0 {
		t.Fatalf("publication lost: %s %s %d", state, gameID, games)
	}
	if _, err := os.Stat(fixture.blobs.Path(source)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	// A fresh application service reads the durable decision without the original request actor.
	if err := fixture.service.reviewApprovals().Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	approved, err := fixture.service.Approve(ctx, itemID, 1)
	if err != nil || approved.GameID != gameID {
		t.Fatalf("recovery=%+v error=%v", approved, err)
	}
	assertApprovalSourcePublishedOnce(t, fixture, itemID, request.Intent.ItemID, gameID)
	assertApprovalSelectionsPublished(t, fixture, gameID)
	published, err := filestore.PublishedRecord(source, itemID, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.blobs.Path(published)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.blobs.Path(source)); !os.IsNotExist(err) {
		t.Fatalf("staging payload remains: %v", err)
	}
}
