package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

func TestESReviewHandoffWritesRollbackTogether(t *testing.T) {
	for _, statement := range []string{
		"UPDATE jobs SET version=version",
		"UPDATE review_drafts SET metadata_json",
		"UPDATE import_items SET search_text",
		"INSERT INTO review_events",
		"UPDATE emulationstation_import_items SET execution_state='REVIEW_PENDING'",
		"UPDATE emulationstation_imports SET skipped_mapping_item_count",
		"INSERT INTO job_events",
	} {
		for _, mode := range []string{"SQL", "count", "zero"} {
			if statement == "INSERT INTO review_events" && mode != "SQL" {
				continue
			}
			t.Run(statement+"/"+mode, func(t *testing.T) {
				fixture := newLifecycleFixture(t)
				_, unit := startLifecycleImport(t, fixture, "", "nes")
				item, ordinary := reserveExecutionReview(t, fixture, unit)
				prepareCancellationCheckpoint(t, fixture, unit, item, ordinary, "attached")
				before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
				var hits atomic.Int64
				faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database,
					reviewHandoffFault(statement, mode, []string{unit.JobID, unit.ImportID, item.ID, ordinary.Items[0].ItemID}, &hits))
				fixture.service.database = faultDB
				err := fixture.service.finalizeReviewHandoff(fixture.context, unit, item,
					ordinary.Created.ImportJobID, ordinary.Items[0].ItemID, nil)
				if err == nil || mode != "zero" && !errors.Is(err, errExecutionReviewFault) || hits.Load() != 1 {
					t.Fatalf("mode=%s error=%v hits=%d", mode, err, hits.Load())
				}
				if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
					t.Fatalf("partial metadata/audit/source/progress write: before=%s after=%s", before, after)
				}
			})
		}
	}
}

func reviewHandoffFault(statement, mode string, identities []string, hits *atomic.Int64) testsupport.SQLFaultHooks {
	matches := func(query string, args []driver.NamedValue) bool {
		if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), statement) {
			return false
		}
		for _, arg := range args {
			for _, id := range identities {
				if arg.Value == id {
					return true
				}
			}
		}
		return false
	}
	return testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if mode == "SQL" && matches(query, args) {
				hits.Add(1)
				return errExecutionReviewFault
			}
			return nil
		},
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if mode == "SQL" && matches(query, args) {
				hits.Add(1)
				return errExecutionReviewFault
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if mode != "SQL" && matches(query, args) {
				hits.Add(1)
				return recoveryReviewResult{Result: result, zero: mode == "zero"}, nil
			}
			return result, nil
		},
	}
}
