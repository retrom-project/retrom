package metadatascrape

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/service/metadatascrape"
)

type mediaFixture struct {
	database *sql.DB
	blobs    *blobstore.Store
	now      time.Time
	jobID    string
	assets   []hasheous.AssetRef
}

type mediaSource func(context.Context, hasheous.AssetRef, int64) (hasheous.AssetData, error)

func (source mediaSource) FetchAssetBounded(ctx context.Context, ref hasheous.AssetRef, limit int64) (hasheous.AssetData, error) {
	return source(ctx, ref, limit)
}

func newMediaFixture(t *testing.T, assets ...hasheous.AssetRef) *mediaFixture {
	t.Helper()
	fixture := newEmptyMediaFixture(t)
	fixture.assets = assets
	if err := fixture.record(t.Context(), NewRecorder(fixture.database)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT media_fetch_job_id FROM scrape_candidate_assets ORDER BY ordinal LIMIT 1`).Scan(&fixture.jobID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *mediaFixture) clock() time.Time { return fixture.now }
func (fixture *mediaFixture) worker(source mediaSource) *metadatascrape.MediaWorker {
	return metadatascrape.NewMediaWorker(NewMedia(fixture.database), source, fixture.blobs, fixture.clock)
}

func (fixture *mediaFixture) snapshot(t *testing.T) metadatascrape.MediaSnapshot {
	t.Helper()
	var snapshot metadatascrape.MediaSnapshot
	err := NewMedia(fixture.database).WithWrite(t.Context(), func(scope metadatascrape.MediaScope) error {
		var err error
		snapshot, err = scope.Read.Snapshot(t.Context(), fixture.jobID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func newEmptyMediaFixture(t *testing.T) *mediaFixture {
	t.Helper()
	fixture := &mediaFixture{database: recoveryDatabase(t), now: recoveryTime}
	var err error
	fixture.blobs, err = blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recoveryExec(t, fixture.database, `INSERT INTO content_hash_evidence
 (id,scrape_run_id,profile,crc32,query_order,payload_released_at_ms,created_at_ms)
 VALUES('evidence','run','RAW_FILE','12345678',0,0,?)`, fixture.now.UnixMilli())
	return fixture
}

func (fixture *mediaFixture) record(ctx context.Context, repository *ResultRepository) error {
	recorder := metadatascrape.NewRecorder(repository, fixture.blobs, fixture.clock)
	assets := fixture.assets
	if len(assets) == 0 {
		assets = []hasheous.AssetRef{{ProviderAssetID: "cover", Kind: "COVER", Path: "/api/v1/images/cover"}}
	}
	processor := recoveryProcess(func(ctx context.Context, claim metadatascrape.WorkerClaim, _ string) (int, string, error) {
		created, err := recorder.Record(ctx, metadatascrape.LookupAttempt{
			Claim: claim, EvidenceID: "evidence", AttemptNo: 1,
			AllowCandidate: true, Lookup: metadatascrape.ResolvedLookup{Result: hasheous.LookupResult{
				Outcome: hasheous.OutcomeHit, RequestDigest: strings.Repeat("d", 64), Candidate: &hasheous.Candidate{
					ProviderGameID: "17", Assets: assets,
				},
			}},
		})
		if err != nil || !created {
			return 0, "CANDIDATE_FAILED", err
		}
		return 1, "", nil
	})
	return metadatascrape.NewWorker(NewWorker(fixture.database), processor, fixture.clock).Run(ctx, "run")
}
