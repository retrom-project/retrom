//go:build integration

package metadatascrape_test

import (
	"database/sql"
	"testing"

	"retrom/internal/composition"
	"retrom/internal/libraryimport"
	jobpersistence "retrom/internal/persistence/jobs"
	"retrom/internal/service/jobs"
)

func waitMetadataMedia(t *testing.T, database *sql.DB, metadataJobID string) {
	t.Helper()
	var id string
	err := database.QueryRowContext(t.Context(), `SELECT j.id FROM jobs j JOIN scrape_candidate_assets a ON a.media_fetch_job_id=j.id
 JOIN scrape_candidates c ON c.id=a.scrape_candidate_id JOIN metadata_scrape_runs r ON r.id=c.scrape_run_id
 WHERE r.job_id=?`, metadataJobID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, database.QueryRowContext, `SELECT state FROM jobs WHERE id=?`, id, "SUCCEEDED")
}

func selectReadyReviewMedia(t *testing.T, database *sql.DB, importer *libraryimport.Service, itemID, importID, assetID string) int64 {
	t.Helper()
	evidence, err := composition.NewMetadataEvidenceQueries(database).Review(t.Context(), itemID)
	if err != nil {
		t.Fatal(err)
	}
	ready := false
	for _, candidate := range evidence.Candidates {
		for _, asset := range candidate.Assets {
			if asset.ID == assetID && asset.Status == "READY" {
				ready = true
			}
		}
	}
	if !ready {
		t.Fatal("current candidate query did not expose completed media")
	}
	batch, err := jobs.New(jobpersistence.New(database), mediaFixtureNow).ImportEvents(t.Context(), importID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var completionID int64
	if err := database.QueryRowContext(t.Context(), `SELECT e.id FROM job_events e JOIN scrape_candidate_assets a ON a.media_fetch_job_id=e.job_id WHERE a.id=? AND e.event_type='SUCCEEDED'`, assetID).Scan(&completionID); err != nil {
		t.Fatal(err)
	}
	completed := false
	for _, event := range batch.Events {
		if event.ID == completionID && event.Type == "SUCCEEDED" {
			completed = true
		}
	}
	if !completed {
		t.Fatal("import refresh omitted durable media completion")
	}
	result, err := importer.PatchDraft(t.Context(), itemID, 1, libraryimport.DraftPatch{
		TagIDs: []string{}, SelectedAssets: &libraryimport.SelectedAssets{CoverCandidateAssetID: &assetID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var selected string
	if err := database.QueryRowContext(t.Context(), `SELECT cover_candidate_asset_id FROM import_items WHERE id=?`, itemID).Scan(&selected); err != nil {
		t.Fatal(err)
	}
	if selected != assetID {
		t.Fatalf("explicit media selection=%s", selected)
	}
	return result.Version
}
