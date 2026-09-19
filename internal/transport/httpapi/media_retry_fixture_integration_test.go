//go:build integration

package httpapi

import (
	"context"
	"strings"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	metadatamodel "retrom/internal/model/metadata"
	metadatascrapemodel "retrom/internal/model/metadatascrape"

	metadatapersistence "retrom/internal/repo/metadatascrape"
	"retrom/internal/service/metadatascrape"
)

type retryMediaSource struct{}

func (retryMediaSource) FetchAssetBounded(context.Context, metadatamodel.AssetReference, int64) (metadatamodel.AssetData, error) {
	return metadatamodel.AssetData{Bytes: []byte("deterministic media"), ReceivedBytes: 18, MediaType: "image/png", Width: 1, Height: 1}, nil
}

type retryMediaProcessor struct {
	recorder *metadatascrape.ResultRecorder
}

func (processor retryMediaProcessor) Process(ctx context.Context, claim metadatascrapemodel.WorkerClaim, _ string) (int, string, error) {
	_, err := processor.recorder.Record(ctx, metadatascrapemodel.LookupAttempt{
		Claim: claim, EvidenceID: "media-evidence", AttemptNo: 1,
		AllowCandidate: true, Lookup: metadatascrapemodel.ResolvedLookup{Result: metadatamodel.LookupResult{
			Outcome: metadatamodel.OutcomeHit, RequestDigest: strings.Repeat("9", 64), Candidate: &metadatamodel.Candidate{
				ProviderGameID: "media",
				Assets:         []metadatamodel.AssetReference{{ProviderAssetID: "cover", Kind: "COVER", Path: "/api/v1/images/cover"}},
			},
		}},
	})
	return 1, "MEDIA_FIXTURE_FAILED", err
}

func newMediaRetryFixture(t *testing.T) validationRetryFixture {
	t.Helper()
	fixture := newValidationRetryFixture(t)
	database := fixture.server.database
	now := fixture.now().UnixMilli()
	gameID := "01980000-0000-7000-8000-000000000191"
	err := metadatapersistence.NewScheduler(database).WithWrite(t.Context(), func(scope metadatascrapemodel.ScheduleScope) error {
		return scope.Writes.Create(t.Context(), metadatascrapemodel.SchedulePlan{
			Subject: metadatascrapemodel.Subject{Kind: "GAME", ID: gameID},
			RunID:   "media-run", JobID: "metadata-job", Provider: "HASHEOUS", Dedupe: strings.Repeat("8", 64), PayloadJSON: `{}`,
			JobState: "QUEUED", RunState: "RUNNING", EventJSON: `{}`, Now: now,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	validationRetrySQL(t, database, `INSERT INTO content_hash_evidence
 (id,scrape_run_id,profile,crc32,query_order,payload_released_at_ms,created_at_ms)
 VALUES('media-evidence','media-run','RAW_FILE','12345678',0,0,?)`, now)
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recorder := metadatascrape.NewRecorder(metadatapersistence.NewRecorder(database), blobs, fixture.now)
	metadata := metadatascrape.NewWorker(metadatapersistence.NewWorker(database), retryMediaProcessor{recorder}, fixture.now)
	if err := metadata.Run(t.Context(), "media-run"); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT id FROM jobs WHERE kind='MEDIA_FETCH'`).Scan(&fixture.jobID); err != nil {
		t.Fatal(err)
	}
	validationRetrySQL(t, database, `UPDATE jobs SET state='FAILED',error_code='MEDIA_BLOB_FAILED',error_retryable=1,finished_at_ms=? WHERE id=?`, now, fixture.jobID)
	worker := metadatascrape.NewMediaWorker(metadatapersistence.NewMedia(database), retryMediaSource{}, blobs, fixture.now)
	fixture.server.metadata = metadatascrape.NewWithMedia(nil, metadata, worker, fixture.now)
	t.Cleanup(fixture.server.metadata.Close)
	return fixture
}
