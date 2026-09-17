package maintenance

import (
	"testing"
	"time"

	"retrom/internal/adapter/integration/payloadrelease"
)

func TestRestoredSourceFailureSatisfiesStartupPayloadLifecycle(t *testing.T) {
	t.Parallel()
	db := restoreReviewFixture(t, "EMULATIONSTATION")
	if err := runReviewRestoreTransaction(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	service, err := payloadrelease.New(db, nil, func() time.Time { return time.UnixMilli(10) }, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("restored source prevents application startup: %v", err)
	}
	service.Close()
	var sourceState, payloadState, kind, scopeType, scopeID string
	err = db.QueryRowContext(t.Context(), `SELECT source.execution_state,source.payload_state,
COALESCE(job.kind,''),COALESCE(job.scope_type,''),COALESCE(job.scope_id,'')
FROM pegasus_import_items source LEFT JOIN jobs job ON job.id=source.payload_release_job_id WHERE source.id='item'`).Scan(
		&sourceState, &payloadState, &kind, &scopeType, &scopeID,
	)
	if err != nil || sourceState != "COMMIT_FAILED" || payloadState != "RELEASING" || kind != "PAYLOAD_RELEASE" ||
		scopeType != "PEGASUS_IMPORT_ITEM" || scopeID != "item" {
		t.Fatalf("restored terminal payload is not scheduled: %s/%s/%s/%s/%s %v",
			sourceState, payloadState, kind, scopeType, scopeID, err)
	}
}
