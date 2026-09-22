package maintenance

import (
	"testing"
)

func TestRestoreCompletesEveryReservedReviewPreparationState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"PENDING", "COPYING", "VALIDATING"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			db := restoreReviewFixture(t)
			retryable := state == "SOURCE_CHANGED" || state == "READ_FAILED" || state == "COMMIT_FAILED"
			var code *string
			var completed *int64
			if retryable {
				value := "EMULATIONSTATION_METADATA_HANDOFF_FAILED"
				code = &value
				instant := int64(2)
				completed = &instant
			}
			_, err := db.ExecContext(t.Context(), `UPDATE source_import_items
SET execution_state=?,retryable=?,error_code=?,completed_at_ms=? WHERE id='item'`, state, retryable, code, completed)
			if err != nil {
				t.Fatal(err)
			}
			if err := runReviewRestoreTransaction(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			var state string
			if err := db.QueryRowContext(t.Context(), "SELECT execution_state FROM source_import_items WHERE id='item'").Scan(&state); err != nil || state != "REVIEW_PENDING" {
				t.Fatalf("restored state=%s err=%v", state, err)
			}
		})
	}
}

func TestRestoreRetainsManualMetadataAfterCompletedHandoff(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"SOURCE"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db := restoreReviewFixture(t)
			table, id := "source_import_items", "item"
			if kind == "SOURCE" {
				table, id = "source_import_items", "item"
			}
			_, err := db.ExecContext(t.Context(), `UPDATE `+table+` SET execution_state='REVIEW_PENDING',completed_at_ms=2,
library_import_job_id='handoff-job',library_import_item_id='handoff-item' WHERE id=?`, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := runReviewRestoreTransaction(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			var title string
			var events, payloads int
			err = db.QueryRowContext(t.Context(), `SELECT json_extract(metadata_json,'$.title'),
(SELECT version-1 FROM review_drafts WHERE import_item_id='handoff-item'),
(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE' AND scope_id IN ('handoff-item','handoff-job',?))
FROM review_drafts WHERE import_item_id='handoff-item'`, id).Scan(&title, &events, &payloads)
			if err != nil || title != "Original" || events != 0 || payloads != 0 {
				t.Fatalf("restore overwrote user decision: title=%q events=%d payloads=%d err=%v", title, events, payloads, err)
			}
		})
	}
}
