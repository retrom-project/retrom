//go:build integration

package libraryimport

import (
	"context"
	"testing"

	"retrom/internal/authn"
)

const approvalActorID = "01980000-0000-7000-8000-000000009611"

func prepareApprovalSelections(t *testing.T, fixture deduplicateFixture, itemID string) context.Context {
	t.Helper()
	fixture.execute(t, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('approval-selection-profile','Approval selection',1)`)
	fixture.execute(t, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES(?,'approval-selection-profile','approval.selection','Approval selection','ADMIN','ENABLED',1,1)`, approvalActorID)
	tag, err := fixture.service.tags.Create(t.Context(), approvalActorID, "Selected approval tag")
	if err != nil {
		t.Fatal(err)
	}
	fixture.execute(t, `INSERT INTO review_draft_tags(review_draft_id,tag_id,assigned_by_user_id,created_at_ms)
 SELECT id,?,?,? FROM review_drafts WHERE import_item_id=?`, tag.TagID, approvalActorID, fixture.service.now().UnixMilli(), itemID)
	fixture.execute(t, `INSERT INTO review_uploaded_assets(id,import_item_id,upload_file_id,blob_id,kind,width_px,height_px,media_type,created_at_ms)
 SELECT 'approval-selected-cover',?,file.id,file.final_blob_id,'COVER',1,1,'image/png',?
 FROM upload_files file JOIN import_jobs parent ON parent.upload_session_id=file.upload_session_id
 JOIN import_items item ON item.import_job_id=parent.id WHERE item.id=? LIMIT 1`, itemID, fixture.service.now().UnixMilli(), itemID)
	fixture.execute(t, `UPDATE review_drafts SET cover_uploaded_asset_id='approval-selected-cover' WHERE import_item_id=?`, itemID)
	return authn.WithPrincipal(t.Context(), authn.Principal{UserID: approvalActorID})
}

func assertApprovalSelectionsPublished(t *testing.T, fixture deduplicateFixture, gameID string) {
	t.Helper()
	var covers, tags int
	var actor string
	err := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM game_assets WHERE game_id=? AND kind='COVER'),
 (SELECT count(*) FROM game_tags WHERE game_id=?),
 (SELECT assigned_by_user_id FROM game_tags WHERE game_id=? LIMIT 1)`, gameID, gameID, gameID).
		Scan(&covers, &tags, &actor)
	if err != nil {
		t.Fatal(err)
	}
	if covers != 1 || tags != 1 || actor != approvalActorID {
		t.Fatalf("cover=%d tags=%d actor=%s", covers, tags, actor)
	}
}
