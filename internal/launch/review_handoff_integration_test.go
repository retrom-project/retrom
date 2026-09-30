//go:build integration

package launch

import (
	"context"
	"errors"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	mediarepo "retrom/internal/persistence/mediaaccess"
	"retrom/internal/service/mediaaccess"
	"retrom/internal/testsupport/workflowfixture"
)

func assertCompletedReviewHandoff(t *testing.T, database dbapi.DB, service *Service,
	preview ReviewPreviewCreated, gameID string,
) {
	t.Helper()
	t.Run("publication closes preview before cleanup", func(t *testing.T) {
		if _, err := service.ReviewPreviewConfig(context.Background(), preview.PreviewID, preview.Capability); err == nil {
			t.Fatal("completed review still authorizes preview config before payload cleanup")
		}
	})
	t.Run("publication adopts captured screenshot", func(t *testing.T) {
		var screenshots int
		if err := dbapi.QueryRowContext(context.Background(), database,
			`SELECT count(*) FROM game_assets WHERE game_id=? AND kind='SCREENSHOT'`, gameID).Scan(&screenshots); err != nil {
			t.Fatal(err)
		}
		if screenshots != 1 {
			t.Fatalf("published screenshot assets=%d, want captured runtime screenshot", screenshots)
		}
	})
	t.Run("publication closes review media before cleanup", func(t *testing.T) {
		var id string
		if err := dbapi.QueryRowContext(t.Context(), database, `SELECT screenshot.id FROM review_runtime_screenshots screenshot
 JOIN review_preview_bindings binding ON binding.import_item_id=screenshot.import_item_id
 WHERE binding.preview_session_id=?`, preview.PreviewID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := mediaaccess.New(mediarepo.New(database)).Review(t.Context(), id, ""); !errors.Is(err, mediaaccess.ErrNotFound) {
			t.Fatalf("completed review still authorizes captured media: %v", err)
		}
	})
}

func assertPublishedMediaAfterProcessDeletion(t *testing.T, database dbapi.DB, files *filestore.Store,
	itemID, gameID string,
) {
	t.Helper()
	workflowfixture.DeleteFinishedProcess(t, database, files, itemID)
	var assetID string
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT id FROM game_assets
 WHERE game_id=? AND kind='SCREENSHOT'`, gameID).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	asset, err := mediaaccess.New(mediarepo.New(database)).Game(t.Context(), assetID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := files.OpenRecord(asset.FileRecord)
	if err != nil {
		t.Fatalf("adopted media depends on deleted process files: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
