package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/hasheous"
	"retrom/internal/payloadrelease"
	"retrom/internal/service/metadatascrape"
)

func TestGameDeletionCancelsMediaBeforeActualPayloadRelease(t *testing.T) {
	fixture := newMediaFixture(t)
	tx, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = payloadrelease.ScheduleGameDeletion(t.Context(), tx, "game", 1, fixture.now.UnixMilli())
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	source := mediaSource(func(context.Context, hasheous.AssetRef, int64) (hasheous.AssetData, error) {
		t.Fatal("deleted game downloaded media")
		return hasheous.AssetData{}, nil
	})
	if err := fixture.worker(source).Run(t.Context(), fixture.jobID); !errors.Is(err, metadatascrape.ErrGameDeleted) {
		t.Fatal(err)
	}
	snapshot := fixture.snapshot(t)
	if snapshot.Job.State != "CANCELLED" || snapshot.Asset.Status != "CANCELLED" {
		t.Fatalf("deleted media=%+v", snapshot)
	}
	release, err := payloadrelease.New(fixture.database, fixture.blobs, fixture.clock, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release.Close)
	if did, err := release.RunOnce(t.Context()); err != nil || !did {
		t.Fatalf("actual payload release=%v/%v", did, err)
	}
	var state string
	var assets int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT payload_state,(SELECT count(*) FROM scrape_candidate_assets)
 FROM games WHERE id='game'`).Scan(&state, &assets); err != nil {
		t.Fatal(err)
	}
	if state != "RELEASED" || assets != 0 {
		t.Fatalf("release blocked by media relation: %s/%d", state, assets)
	}
	if err := fixture.worker(source).Run(t.Context(), fixture.jobID); err != nil {
		t.Fatal(err)
	}
}
