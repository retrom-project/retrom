package metadatascrape

import (
	"database/sql"
	"errors"
	"testing"
)

func TestMediaScheduleRejectsParentJobWithForeignScope(t *testing.T) {
	fixture := newEmptyMediaFixture(t)
	recoveryExec(t, fixture.database, `UPDATE jobs SET scope_id='another-game' WHERE id='job'`)
	if err := fixture.record(t.Context(), NewRecorder(fixture.database)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign job scope accepted: %v", err)
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE kind='MEDIA_FETCH'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("foreign media jobs=%d", count)
	}
}

func TestMediaJobAssociationIsUnique(t *testing.T) {
	fixture := newMediaFixture(t)
	snapshot := fixture.snapshot(t)
	_, err := fixture.database.ExecContext(t.Context(), `INSERT INTO scrape_candidate_assets(id,scrape_candidate_id,
 provider_response_id,provider_asset_id,kind_hint,ordinal,source_path,status,created_at_ms,updated_at_ms,media_fetch_job_id)
 SELECT 'duplicate',scrape_candidate_id,provider_response_id,'another','COVER',1,'/api/v1/images/another',
 'PENDING',created_at_ms,updated_at_ms,media_fetch_job_id FROM scrape_candidate_assets WHERE id=?`, snapshot.Asset.ID)
	if err == nil {
		t.Fatal("two candidate assets shared one download Job")
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM scrape_candidate_assets`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("association count=%d", count)
	}
}
