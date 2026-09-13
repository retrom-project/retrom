package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/adapter/integration/libraryimport"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestESReviewHandoffRejectsReplacedWorker(t *testing.T) {
	for _, checkpoint := range []string{"attached", "metadata"} {
		t.Run(checkpoint, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			_, unit := startLifecycleImport(t, fixture, "", "nes")
			item, ordinary := reserveExecutionReview(t, fixture, unit)
			prepareCancellationCheckpoint(t, fixture, unit, item, ordinary, checkpoint)
			if _, err := fixture.database.ExecContext(
				fixture.context,
				`UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id=?`,
				unit.JobID,
			); err != nil {
				t.Fatal(err)
			}
			before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
			err := fixture.service.finalizeReviewHandoff(
				fixture.context,
				unit,
				item,
				ordinary.Created.ImportJobID,
				ordinary.Items[0].ItemID,
				nil,
			)
			if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
				t.Fatalf("stale handoff retained writes: error=%v before=%s after=%s", err, before, after)
			}
			if err == nil {
				t.Fatal("stale handoff reported success")
			}
		})
	}
}

func TestESReviewHandoffRejectsWrongOrdinaryIdentity(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, ordinary := reserveExecutionReview(t, fixture, unit)
	prepareCancellationCheckpoint(t, fixture, unit, item, ordinary, "metadata")
	before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
	err := fixture.service.finalizeReviewHandoff(fixture.context, unit, item, "other-job", "other-item", nil)
	if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
		t.Fatalf("wrong identity retained handoff: error=%v before=%s after=%s", err, before, after)
	}
	if err == nil {
		t.Fatal("wrong ordinary handoff reported success")
	}
}

func TestESReviewHandoffFailureRollsBackSeedAndRetainsCause(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, ordinary := reserveExecutionReview(t, fixture, unit)
	prepareCancellationCheckpoint(t, fixture, unit, item, ordinary, "attached")
	if err := fixture.service.updateExecutionPhase(fixture.context, unit, "PREPARING_REVIEWS"); err != nil {
		t.Fatal(err)
	}
	before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
	cause := errors.New("final review handoff failed")
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(
		t,
		fixture.database,
		testsupport.SQLFaultHooks{BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			normalized := strings.Join(strings.Fields(query), " ")
			if !strings.HasPrefix(normalized, "UPDATE emulationstation_import_items SET execution_state='REVIEW_PENDING'") {
				return nil
			}
			for _, arg := range args {
				if arg.Value == item.ID {
					hits.Add(1)
					return cause
				}
			}
			return nil
		}},
	)
	fixture.service.database = faultDB
	err := fixture.service.prepareLibraryReview(
		fixture.context,
		unit,
		item,
		ordinary.Created.ImportJobID,
		ordinary.Items[0],
	)
	if !errors.Is(err, cause) || hits.Load() != 1 {
		t.Errorf("handoff error=%v hits=%d", err, hits.Load())
	}
	if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
		t.Fatalf("handoff failure retained metadata/source projection: before=%s after=%s", before, after)
	}
}

func (service *Service) finalizeReviewHandoff(ctx context.Context, unit work, item executionItem,
	jobID, itemID string, _ []libraryimport.ServerMetadataWarning,
) error {
	return service.reviewHandoff().Complete(ctx, application.ReviewHandoffRequest{
		Execution: unit, ItemID: item.ID, LibraryJobID: jobID, LibraryItemID: itemID,
	})
}
