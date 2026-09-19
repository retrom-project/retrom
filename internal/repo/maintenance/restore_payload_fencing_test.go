package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"
	persistence "retrom/internal/repo/payloadrelease"
	release "retrom/internal/service/payloadrelease"
)

type changedPayloadOwner struct {
	payloadreleasemodel.SchedulingScope

	transaction *sql.Tx
	mutation    string
	changed     bool
}

func (records *changedPayloadOwner) Owner(ctx context.Context, ref payloadreleasemodel.Scope) (payloadreleasemodel.Owner, error) {
	before, err := records.SchedulingScope.Owner(ctx, ref)
	if err != nil || records.changed {
		return before, err
	}
	records.changed = true
	_, err = records.transaction.ExecContext(ctx, records.mutation)
	return before, err
}

func TestSourcePayloadSchedulingFencesEveryOwnerFact(t *testing.T) {
	t.Parallel()
	for _, change := range []struct{ name, mutation string }{
		{"version", "version=version+1"},
		{"terminal", "execution_state='REVIEW_PENDING'"},
		{"retryable", "retryable=1"},
		{"ordinary-binding", "library_import_job_id='handoff-job',library_import_item_id='handoff-item'"},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			db := restoreReviewFixture(t, "EMULATIONSTATION")
			if _, err := db.ExecContext(t.Context(), `UPDATE pegasus_import_items
SET execution_state='COMMIT_FAILED',retryable=0,completed_at_ms=10 WHERE id='item'`); err != nil {
				t.Fatal(err)
			}
			before := reviewRestoreSnapshot(t, db)
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer dbexec.Rollback(tx)
			scope := &changedPayloadOwner{
				SchedulingScope: persistence.BindScheduling(tx), transaction: tx,
				mutation: "UPDATE pegasus_import_items SET " + change.mutation + " WHERE id='item'",
			}
			id, err := release.NewScheduler(nil).TerminalSource(t.Context(), scope, payloadreleasemodel.Scope{Type: payloadreleasemodel.ScopePegasusImportItem, ID: "item"}, 10)
			if id != "" || !errors.Is(err, payloadreleasemodel.ErrScopeInvalid) || !scope.changed {
				t.Fatalf("payload scheduler ignored stale owner: %q/%v changed=%t", id, err, scope.changed)
			}
			var jobs int
			if err := tx.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs
WHERE kind='PAYLOAD_RELEASE' AND scope_type='PEGASUS_IMPORT_ITEM' AND scope_id='item'`).Scan(&jobs); err != nil || jobs != 1 {
				t.Fatalf("owner fence did not follow actual job writes: %d/%v", jobs, err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if after := reviewRestoreSnapshot(t, db); after != before {
				t.Fatal("lost owner fence retained a partial job or changed owner")
			}
		})
	}
}
