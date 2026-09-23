package metadatascrape

import (
	"strings"
	"testing"

	application "retrom/internal/service/metadatascrape"
	"retrom/internal/testsupport"
)

func TestReplacingReviewScrapeClearsOldSelectionsAndPreservesEditedText(t *testing.T) {
	t.Parallel()
	fixture := newMediaFixture(t)
	seedReviewScrapeSelection(t, fixture)
	now := fixture.now.UnixMilli()
	plan := application.SchedulePlan{
		Subject: application.Subject{Kind: "IMPORT_ITEM", ID: "item"},
		RunID:   "replacement", JobID: "replacement-job", Provider: "NONE", Dedupe: strings.Repeat("e", 64),
		PayloadJSON: "{}", JobState: "SUCCEEDED", RunState: "COMPLETED", EventJSON: "{}", Now: now, FinishedAt: &now,
	}
	if err := NewScheduler(fixture.database).WithWrite(t.Context(), func(scope application.ScheduleScope) error {
		return scope.Writes.Create(t.Context(), plan)
	}); err != nil {
		t.Fatal(err)
	}
	var runs, candidates, screenshots int
	var selected bool
	var metadata string
	err := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM metadata_scrape_runs WHERE import_item_id='item'),
 (SELECT count(*) FROM scrape_candidates),
 (SELECT count(*) FROM review_draft_screenshot_assets),
 selected_candidate_id IS NOT NULL OR cover_candidate_asset_id IS NOT NULL OR background_candidate_asset_id IS NOT NULL,
 metadata_json FROM import_items WHERE id='item'`).Scan(&runs, &candidates, &screenshots, &selected, &metadata)
	if err != nil || runs != 1 || candidates != 0 || screenshots != 0 || selected || metadata != `{"title":"Edited title"}` {
		t.Fatalf("replacement lost current draft: runs=%d candidates=%d screenshots=%d selected=%v metadata=%s err=%v",
			runs, candidates, screenshots, selected, metadata, err)
	}
}

func seedReviewScrapeSelection(t *testing.T, fixture *mediaFixture) {
	t.Helper()
	target, err := testsupport.LookupRuntimeTarget(t.Context(), fixture.database, "mgba")
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("f", 64)
	recoveryExec(t, fixture.database, `INSERT INTO upload_sessions
 (id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES('upload','COMPLETE','FILES',1,0,?,100,1,1)`, digest)
	recoveryExec(t, fixture.database, `INSERT INTO import_jobs
 (id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
 provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,created_at_ms,updated_at_ms)
 SELECT 'import','upload',platform_instance_id,1,'gba','mgba',?,?,'NONE','{}',?,'COMPLETED',1,1
 FROM games WHERE id='game'`, target.ProviderID, target.TargetID, digest)
	recoveryExec(t, fixture.database, `INSERT INTO import_items
 (id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
 VALUES('item','import',?,'REVIEW_PENDING','{}',?,'Edited title',1,1)`, digest, digest)
	recoveryExec(t, fixture.database, `UPDATE metadata_scrape_runs SET game_id=NULL,import_item_id='item' WHERE id='run'`)
	recoveryExec(t, fixture.database, `UPDATE import_items SET
 target_platform_instance_id=(SELECT platform_instance_id FROM games WHERE id='game'),
 selected_candidate_id=(SELECT scrape_candidate_id FROM scrape_candidate_assets LIMIT 1),
 cover_candidate_asset_id=(SELECT id FROM scrape_candidate_assets LIMIT 1),
 background_candidate_asset_id=(SELECT id FROM scrape_candidate_assets LIMIT 1),
 metadata_json='{"title":"Edited title"}',review_version=1,review_created_at_ms=1,review_updated_at_ms=1
 WHERE id='item'`)
	recoveryExec(t, fixture.database, `INSERT INTO review_draft_screenshot_assets(review_draft_id,ordinal,candidate_asset_id,created_at_ms)
 SELECT 'item',0,id,1 FROM scrape_candidate_assets LIMIT 1`)
}
