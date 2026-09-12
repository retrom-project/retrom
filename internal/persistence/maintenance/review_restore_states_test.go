package maintenance

import (
	"database/sql"
	"fmt"
	"testing"
)

func TestRestoreCompletesEveryReservedReviewPreparationState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"PENDING", "COPYING", "VALIDATING", "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			db := restoreReviewFixture(t, "EMULATIONSTATION")
			retryable := state == "SOURCE_CHANGED" || state == "READ_FAILED" || state == "COMMIT_FAILED"
			var code *string
			var completed *int64
			if retryable {
				value := "EMULATIONSTATION_METADATA_HANDOFF_FAILED"
				code = &value
				instant := int64(2)
				completed = &instant
			}
			_, err := db.ExecContext(t.Context(), `UPDATE emulationstation_import_items
SET execution_state=?,retryable=?,error_code=?,completed_at_ms=? WHERE id='es-item'`, state, retryable, code, completed)
			if err != nil {
				t.Fatal(err)
			}
			if err := runReviewRestoreTransaction(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			assertRestoredEmulationStationReview(t, db)
		})
	}
}

func TestRestoreRetainsManualMetadataAfterCompletedHandoff(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"PEGASUS", "EMULATIONSTATION"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db := restoreReviewFixture(t, kind)
			table, id := "pegasus_import_items", "item"
			if kind == "EMULATIONSTATION" {
				table, id = "emulationstation_import_items", "es-item"
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
(SELECT count(*) FROM review_events WHERE import_item_id='handoff-item'),
(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE')
FROM review_drafts WHERE import_item_id='handoff-item'`).Scan(&title, &events, &payloads)
			if err != nil || title != "Original" || events != 0 || payloads != 0 {
				t.Fatalf("restore overwrote user decision: title=%q events=%d payloads=%d err=%v", title, events, payloads, err)
			}
		})
	}
}

func TestRestoreUsesFrozenEmulationStationReleaseYear(t *testing.T) {
	t.Parallel()
	for _, year := range []int{1980, 1982} {
		t.Run(fmt.Sprint(year), func(t *testing.T) {
			t.Parallel()
			db := restoreReviewFixture(t, "EMULATIONSTATION")
			if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_imports SET release_year_max=1981
WHERE id='es-import'`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_import_items
SET metadata_json=json_set(metadata_json,'$.releaseYear',?) WHERE id='es-item'`, year); err != nil {
				t.Fatal(err)
			}
			if err := runReviewRestoreTransaction(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			var actual sql.NullInt64
			var warnings int
			err := db.QueryRowContext(t.Context(), `SELECT json_extract(draft.metadata_json,'$.releaseYear'),
(SELECT count(*) FROM json_each(source.warnings_json) WHERE json_extract(value,'$.field')='releaseYear'
AND json_extract(value,'$.code')='FIELD_VALUE_INVALID')
FROM review_drafts draft JOIN emulationstation_import_items source ON source.library_import_item_id=draft.import_item_id
WHERE source.id='es-item'`).Scan(&actual, &warnings)
			if err != nil {
				t.Fatal(err)
			}
			if year == 1980 && (!actual.Valid || actual.Int64 != 1980 || warnings != 0) ||
				year == 1982 && (actual.Valid || warnings != 1) {
				t.Fatalf("restore used current clock instead of frozen year: value=%+v warnings=%d", actual, warnings)
			}
		})
	}
}
