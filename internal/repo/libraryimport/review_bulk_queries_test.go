package libraryimport

import (
	"database/sql"
	"strings"
	"testing"

	application "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func insertReviewBulkQueryFixture(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	instance := testsupport.MustPlatformInstanceID(t, db, "gba/mgba")
	var coreID, providerID, targetID string
	if err := db.QueryRowContext(t.Context(), `
SELECT instance.default_core_id,binding.provider_id,binding.target_id
FROM platform_instances instance
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id
WHERE instance.id=? LIMIT 1`, instance).Scan(&coreID, &providerID, &targetID); err != nil {
		t.Fatal(err)
	}
	const validationID = "validation"
	digest := strings.Repeat("a", 64)
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO import_item_core_validations(
id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,provider_id,target_id,
source_manifest_digest,prepublish_input_digest,status,compatibility_code,dependency_snapshot_json,created_at_ms,source_snapshot_id
)
VALUES(?,?,?,?,?,?,?,? ,?,'READY','READY','{}',1,?)`, validationID, "item", instance, 1, coreID, providerID, targetID,
		digest, digest, "snapshot"); err != nil {
		t.Fatal(err)
	}
	bulkID := "019b0000-0000-7000-8000-000000000010"
	jobID := "019b0000-0000-7000-8000-000000000011"
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'REVIEW_BULK_APPROVAL',?,'REVIEW_BULK_APPROVE',?,1,'{}',1,'QUEUED',0,4,1,1,1,1)`, jobID, bulkID, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO review_bulk_approvals(id,job_id,state,scope_json,scope_digest,candidate_manifest_digest,
matched_count,candidate_count,screenshot_only_count,duplicate_count,attachment_active_count,
source_flagged_count,not_ready_or_stale_count,created_by_user_id,version,created_at_ms,updated_at_ms)
VALUES(?,?, 'QUEUED','{}',?,?,1,1,0,0,0,0,0,'actor',1,1,1)`, bulkID, jobID, digest, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO review_bulk_approval_items(bulk_approval_id,import_item_id,ordinal,expected_review_version,
expected_validation_id,expected_source_snapshot_id,title_snapshot,target_platform_instance_id,
target_platform_name_snapshot,state,created_at_ms)
VALUES(?,?,0,7,?,?,?,? ,?,'PENDING',1)`, bulkID, "item", validationID, "snapshot", "Before", instance, "Game Boy Advance"); err != nil {
		t.Fatal(err)
	}
	return bulkID, instance
}

func TestReviewBulkQueriesRepositoryReadsTypedCandidateRecords(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	_, instance := insertReviewBulkQueryFixture(t, db)
	repository := NewReviewBulkQueries(db)
	candidates, err := repository.Candidates(t.Context(), application.ReviewBulkCandidateQuery{
		Scope: application.ReviewBulkScope{PlatformInstanceID: instance}, Limit: 2,
	})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	if candidates[0].ItemID != "item" || candidates[0].Title != "Before" || candidates[0].ValidationID == nil ||
		*candidates[0].ValidationID != "validation" || candidates[0].ValidationStatus == nil ||
		*candidates[0].ValidationStatus != "READY" {
		t.Fatalf("candidate=%#v", candidates[0])
	}
}

func TestReviewBulkQueriesRepositoryReadsTypedItemsAndSummaries(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	bulkID, _ := insertReviewBulkQueryFixture(t, db)
	repository := NewReviewBulkQueries(db)
	items, err := repository.Items(t.Context(), application.ReviewBulkItemQuery{
		BulkApprovalID: bulkID, AfterOrdinal: -1, Limit: 2,
	})
	if err != nil || len(items) != 1 || items[0].ImportItemID != "item" || items[0].Ordinal != 0 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	summary, err := repository.Summary(t.Context(), bulkID)
	if err != nil || summary.BulkApprovalID != bulkID || summary.State != "QUEUED" || summary.Counts.Matched != 1 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
	active, found, err := repository.ActiveSummary(t.Context())
	if err != nil || !found || active.BulkApprovalID != bulkID {
		t.Fatalf("active=%#v err=%v", active, err)
	}
}

func TestReviewBulkQueriesRepositoryReturnsNoActiveSummary(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	active, found, err := NewReviewBulkQueries(db).ActiveSummary(t.Context())
	if err != nil || found {
		t.Fatalf("active=%#v err=%v", active, err)
	}
}

func TestReviewBulkCandidateStatementAddsBoundedCursor(t *testing.T) {
	t.Parallel()
	query, args, err := reviewBulkCandidateStatement(application.ReviewBulkCandidateQuery{
		AfterItemID:   "019b0000-0000-7000-8000-000000000001",
		ThroughItemID: "019b0000-0000-7000-8000-000000000002",
		Limit:         2,
	})
	if err != nil || !strings.Contains(query, "item.id>?") || !strings.Contains(query, "item.id<=?") || len(args) != 3 {
		t.Fatalf("query=%s args=%#v err=%v", query, args, err)
	}
}
