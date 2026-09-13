package payloadrelease

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestReleaseOrdinaryBindingWritesRollBackSourcePayload(t *testing.T) {
	for _, stage := range []effectWriteFailure{{"UPDATE pegasus_import_item_files", "effect-source"}, {"UPDATE pegasus_import_items SET payload_state='RELEASED'", "effect-source"}} {
		for _, mode := range []string{"sql", "count", "zero"} {
			t.Run(stage.prefix+"/"+mode, func(t *testing.T) {
				fixture := queuedReleaseWorker(t)
				seedEffectGamePayload(t, fixture.database)
				ordinaryJob := seedEffectOrdinarySource(t, fixture.database)
				seedEffectBoundPegasus(t, fixture.database)
				if _, err := fixture.database.ExecContext(t.Context(), `UPDATE jobs SET available_at_ms=1000 WHERE id=?`, fixture.jobID); err != nil {
					t.Fatal(err)
				}
				claim := claimEffect(t, fixture)
				if claim.ID != ordinaryJob {
					t.Fatalf("claim=%s want=%s", claim.ID, ordinaryJob)
				}
				cause := errors.New("bound source persistence failure")
				var hits atomic.Int64
				service := effectFaultService(t, fixture, effectGraphWriteFault(stage, mode, cause, &hits))
				err := service.execute(t.Context(), claim)
				if err == nil || hits.Load() != 1 || mode != "zero" && !errors.Is(err, cause) {
					t.Fatalf("binding error=%v hits=%d", err, hits.Load())
				}
				assertBoundEffectRetained(t, fixture)
				if err := fixture.service.execute(t.Context(), claim); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func assertBoundEffectRetained(t *testing.T, fixture releaseWorkerFixture) {
	t.Helper()
	assertEffectGameGraphRetained(t, fixture)
	var ordinary, source string
	var refs int
	err := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT payload_state FROM import_items WHERE id='effect-item'),payload_state,
 (SELECT count(*) FROM import_item_source_files WHERE import_item_id='effect-item')+
 (SELECT count(*) FROM pegasus_import_item_files WHERE item_id='effect-source' AND blob_id='effect-blob')
 FROM pegasus_import_items WHERE id='effect-source'`).Scan(&ordinary, &source, &refs)
	if err != nil || ordinary != "RELEASING" || source != "RELEASING" || refs != 2 {
		t.Fatalf("bound partial ordinary=%s source=%s refs=%d error=%v", ordinary, source, refs, err)
	}
}
