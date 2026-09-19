package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	persistence "retrom/internal/repo/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

var errExecutionReviewFault = errors.New("interrupted review settlement fault")

func TestESInterruptedReviewCancellationRollsBackMetadataAndOwnership(t *testing.T) {
	for _, statement := range []string{
		"UPDATE review_drafts SET", "INSERT INTO review_events(",
		"UPDATE emulationstation_import_items SET execution_state='REVIEW_PENDING'",
	} {
		t.Run(statement, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			started, unit := startLifecycleImport(t, fixture, "", "nes")
			item, imported := reserveExecutionReview(t, fixture, unit)
			requestExecutionCancellation(t, fixture, started.ID)
			before := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID)
			var hits atomic.Int64
			hook := executionReviewFaultHook(statement, item.ID, unit.JobID, imported.Items[0].ItemID, &hits)
			faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{BeforeExec: hook, BeforeQuery: hook})
			repository := persistence.NewExecutionControl(faultDB)
			closed, err := emulationstationimportservice.NewExecutionControl(repository, fixture.service.now).CloseCancelled(fixture.context, unit)
			if closed || !errors.Is(err, errExecutionReviewFault) {
				t.Fatalf("closed=%v cause=%v", closed, err)
			}
			if hits.Load() < 1 {
				t.Fatalf("fault hits=%d", hits.Load())
			}
			if after := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID); after != before {
				t.Fatalf("partial settlement before=%s after=%s", before, after)
			}
		})
	}
}

func executionReviewSnapshot(t *testing.T, fixture lifecycleFixture, unit work, itemID string) string {
	t.Helper()
	var result string
	err := fixture.database.QueryRowContext(fixture.context, `SELECT json_array(draft.metadata_json,draft.version,
item.state,item.search_text,
(SELECT json_group_array(json_array(id,event_type,before_json,after_json)) FROM review_events WHERE import_item_id=item.id),
(SELECT json_group_array(json_array(id,library_import_job_id,library_import_item_id,payload_state,warnings_json))
 FROM emulationstation_import_items WHERE import_id=?),
(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE'))
FROM review_drafts draft JOIN import_items item ON item.id=draft.import_item_id WHERE item.id=?`, unit.ImportID, itemID).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return executionAuthorityState(t, fixture, unit) + result
}

func executionReviewFaultHook(statement, itemID, jobID, ordinaryID string, hits *atomic.Int64) func(context.Context, string, []driver.NamedValue) error {
	return func(_ context.Context, query string, args []driver.NamedValue) error {
		if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), statement) {
			return nil
		}
		for _, arg := range args {
			if arg.Value == itemID || arg.Value == jobID || arg.Value == ordinaryID {
				hits.Add(1)
				return errExecutionReviewFault
			}
		}
		return nil
	}
}
