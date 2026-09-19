package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	persistence "retrom/internal/repo/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestESExpiredReviewRecoveryRollsBackMetadataAndOwnership(t *testing.T) {
	for _, statement := range []string{
		"UPDATE review_drafts SET", "INSERT INTO review_events(",
		"UPDATE emulationstation_import_items SET execution_state='REVIEW_PENDING'",
		"commit", "affected rows", "zero rows",
	} {
		t.Run(statement, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			started, unit := startLifecycleImport(t, fixture, "", "nes")
			item, imported := reserveExecutionReview(t, fixture, unit)
			requestExecutionCancellation(t, fixture, started.ID)
			*fixture.now = fixture.now.Add(time.Minute)
			before := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID)
			var hits atomic.Int64
			var hooks testsupport.SQLFaultHooks
			if statement == "commit" {
				hooks = testsupport.SQLFaultHooks{
					BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
						if strings.TrimSpace(query) == "COMMIT" {
							hits.Add(1)
							return errExecutionReviewFault
						}
						return nil
					},
				}
			} else {
				hook := executionReviewFaultHook(statement, item.ID, unit.JobID, imported.Items[0].ItemID, &hits)
				hooks = recoveryReviewHooks(statement, item.ID, hook, &hits)
			}
			faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database, hooks)
			repository := persistence.NewRecovery(faultDB)
			err := emulationstationimportservice.NewRecovery(repository, fixture.service.now).Recover(fixture.context)
			want := errExecutionReviewFault
			if statement == "zero rows" {
				want = nil
			}
			if !errors.Is(err, want) {
				t.Fatalf("recovery cause=%v", err)
			}
			if hits.Load() != 1 {
				t.Fatalf("fault hits=%d", hits.Load())
			}
			if after := executionReviewSnapshot(t, fixture, unit, imported.Items[0].ItemID); after != before {
				t.Fatalf("partial recovery before=%s after=%s", before, after)
			}
		})
	}
}
