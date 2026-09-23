//go:build integration

package launch

import (
	"strings"
	"testing"

	"retrom/internal/persistence/recordstore"
)

func TestRPGReviewPreviewRuntimeFileBelongsToSelectedValidation(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "runtime-files")
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `
SELECT count(*) FROM review_preview_files
WHERE preview_session_id=? AND role='RUNTIME_FILE' AND blob_id='rpg-index'`, preview.PreviewID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("selected validation runtime file count=%d error=%v", count, err)
	}
	_, err := recordstore.CreateReviewPreviewFiles(t.Context(), fixture.database, `
INSERT INTO review_preview_files(preview_session_id,role,logical_name,blob_id,sort_order,created_at_ms)
VALUES(?,'RUNTIME_FILE','unrelated.bin','rpg-project-a',99,?)`, preview.PreviewID, fixture.now.UnixMilli())
	if err == nil || !strings.Contains(err.Error(), "invalid review preview runtime file") {
		t.Fatalf("unrelated Blob accepted as runtime file: %v", err)
	}
}

func TestRPGReviewPreviewKeepsUniqueASCIICaseFoldContentLookup(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "folded-preview")
	content, err := fixture.launcher.ReviewPreviewProjectContent(t.Context(), preview.PreviewID, preview.Capability, "rpg_rt.LDB")
	if err != nil || content.Digest != strings.Repeat("1", 64) {
		t.Fatalf("ordinary RPG trial lost case-fold lookup: %+v %v", content, err)
	}
	if _, err := fixture.launcher.ReviewPreviewProjectContent(t.Context(), preview.PreviewID, preview.Capability, "../RPG_RT.ldb"); err == nil {
		t.Fatal("case-fold lookup admitted traversal")
	}
	if _, err := fixture.launcher.ContentAuthorized(t.Context(), preview.PreviewID, "rpg_rt.LDB", true); err == nil {
		t.Fatal("exact isolated entry lookup applied RPG project case folding")
	}
	isolated, err := fixture.launcher.RPGProjectContentAuthorized(t.Context(), preview.PreviewID, "rpg_rt.LDB", true)
	if err != nil || isolated.Digest != content.Digest {
		t.Fatalf("authenticated isolated project changed frozen content: %+v %v", isolated, err)
	}
	if _, err := fixture.launcher.RPGProjectContentAuthorized(t.Context(), preview.PreviewID, "../RPG_RT.ldb", true); err == nil {
		t.Fatal("isolated project lookup admitted traversal")
	}
}
