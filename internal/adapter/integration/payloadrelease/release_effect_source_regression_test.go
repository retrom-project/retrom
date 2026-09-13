package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestReleaseGameFollowsBoundSourceToOrdinaryPayload(t *testing.T) {
	fixture := queuedReleaseWorker(t)
	seedEffectGamePayload(t, fixture.database)
	ordinaryJob := seedEffectOrdinarySource(t, fixture.database)
	seedEffectBoundPegasus(t, fixture.database)
	claim := claimEffect(t, fixture)
	if claim.ID != fixture.jobID {
		t.Fatalf("claimed %s instead of game %s", claim.ID, fixture.jobID)
	}
	if err := fixture.service.execute(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	assertEffectGameGraphReleased(t, fixture)
	var ordinary, source, releaseJob string
	var refs int
	err := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT payload_state FROM import_items WHERE id='effect-item'),payload_state,payload_release_job_id,
 (SELECT count(*) FROM import_item_source_files WHERE import_item_id='effect-item')+
 (SELECT count(*) FROM pegasus_import_item_files WHERE item_id='effect-source' AND blob_id IS NOT NULL)
 FROM pegasus_import_items WHERE id='effect-source'`).Scan(&ordinary, &source, &releaseJob, &refs)
	if err != nil || ordinary != "RELEASED" || source != "RELEASED" || releaseJob != ordinaryJob || refs != 0 {
		t.Fatalf("source graph ordinary=%s source=%s job=%s refs=%d error=%v", ordinary, source, releaseJob, refs, err)
	}
}

func TestReleaseGameSourceReadPreservesStorageCause(t *testing.T) {
	fixture := queuedReleaseWorker(t)
	_, err := fixture.database.ExecContext(t.Context(), `UPDATE games SET metadata_source_kind='IMPORT_REVIEW',metadata_source_ref_id='unreadable-item' WHERE id='schedule-game'`)
	if err != nil {
		t.Fatal(err)
	}
	claim := claimEffect(t, fixture)
	cause := errors.New("ordinary source owner unavailable")
	var hits atomic.Int64
	service := effectFaultService(t, fixture, testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
		q := strings.Join(strings.Fields(query), " ")
		if strings.Contains(q, "FROM import_items WHERE id=?") && effectBound(args, "unreadable-item") {
			hits.Add(1)
			return cause
		}
		return nil
	}})
	err = service.execute(t.Context(), claim)
	if !errors.Is(err, cause) || hits.Load() != 1 {
		t.Fatalf("source cause=%v hits=%d", err, hits.Load())
	}
	assertEffectRetained(t, fixture, ScopeGame)
}
