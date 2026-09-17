//go:build integration

package metadatascrape_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/bootstrap/config"
	metadatascrapemodel "retrom/internal/model/metadatascrape"
	maintenancepersistence "retrom/internal/repo/maintenance"
	mediatapersistence "retrom/internal/repo/metadatascrape"
	"retrom/internal/repo/store"
	"retrom/internal/service/maintenance"
	metadatascrapeservice "retrom/internal/service/metadatascrape"
)

func TestMediaBackupRestorePreservesBudgetAndOriginalExecution(t *testing.T) {
	fixture := createMediaImportFixture(t, doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/v1/Lookup/ByHash" {
			return httpResponse(http.StatusOK, "application/json",
				`{"id":73,"name":"Media Result","attributes":[{"attributeName":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"logo","link":"/api/v1/images/logo"}]}`), nil
		}
		return httpResponse(http.StatusOK, "image/png", "invalid image"), nil
	}))
	var id string
	waitForState(t, fixture.database.SQL.QueryRowContext, `SELECT state FROM import_jobs WHERE id=?`, fixture.importID, "REVIEW_PENDING")
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `SELECT j.id FROM jobs j JOIN import_items i ON i.id=j.scope_id
 WHERE i.import_job_id=? AND j.kind='MEDIA_FETCH'`, fixture.importID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	waitForState(t, fixture.database.SQL.QueryRowContext, `SELECT state FROM jobs WHERE id=?`, id, "FAILED")
	fixture.scraper.Close()
	now := mediaFixtureNow().UnixMilli()
	mediaRestoreSQL(t, fixture.database.SQL, `UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,error_code=NULL,error_retryable=NULL,
 worker_id='interrupted',attempt_count=1,execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=? WHERE id=?`,
		now-1790000, now+10000, now-1, id)
	mediaRestoreSQL(t, fixture.database.SQL, `UPDATE metadata_media_runs SET charged_bytes=123`)
	mediaRestoreSQL(t, fixture.database.SQL, `UPDATE scrape_candidate_assets SET status='FETCHING',error_code=NULL,
 media_charged_bytes=123,media_reserved_bytes=123`)
	var number int
	var name, path string
	if err := fixture.database.SQL.QueryRowContext(t.Context(), `PRAGMA database_list`).Scan(&number, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.Close(); err != nil {
		t.Fatal(err)
	}
	restored := backupRestoreMedia(t, path)
	clock := mediaFixtureNow()
	database, err := store.Open(t.Context(), restored, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	snapshot := restoredMediaSnapshot(t, database.SQL, id)
	if snapshot.Job.Deadline != now+10000 || snapshot.Charged != 123 || snapshot.Asset.Reserved != 123 ||
		!snapshot.Frozen || snapshot.Asset.Order != 0 {
		t.Fatalf("restore changed durable execution: %+v", snapshot)
	}
	blobs, err := blobstore.Open(filepath.Dir(restored))
	if err != nil {
		t.Fatal(err)
	}
	worker := metadatascrapeservice.NewMediaWorker(mediatapersistence.NewMedia(database.SQL), restoredMediaSource{}, blobs, func() time.Time { return clock })
	if err := worker.Run(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if err := worker.Run(t.Context(), id); !errors.Is(err, hasheous.ErrAssetDecodeFailed) {
		t.Fatal(err)
	}
	snapshot = restoredMediaSnapshot(t, database.SQL, id)
	if snapshot.Charged != 130 || snapshot.Job.Attempt != 2 || snapshot.Job.Deadline != now+10000 {
		t.Fatalf("restored worker reset original budget/deadline: %+v", snapshot)
	}
}

type restoredMediaSource struct{}

func (restoredMediaSource) FetchAssetBounded(context.Context, hasheous.AssetRef, int64) (hasheous.AssetData, error) {
	return hasheous.AssetData{ReceivedBytes: 7}, hasheous.ErrAssetDecodeFailed
}

func mediaRestoreSQL(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func restoredMediaSnapshot(t *testing.T, database *sql.DB, id string) metadatascrapemodel.MediaSnapshot {
	t.Helper()
	var snapshot metadatascrapemodel.MediaSnapshot
	err := mediatapersistence.NewMedia(database).CommitWrite(t.Context(), func(scope metadatascrapemodel.MediaScope) error {
		var err error
		snapshot, err = scope.Read.Snapshot(t.Context(), id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func backupRestoreMedia(t *testing.T, path string) string {
	t.Helper()
	root := t.TempDir()
	data := filepath.Dir(path)
	if err := os.MkdirAll(filepath.Join(data, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"launch-capability.key", "netplay-capability.key"} {
		if err := os.WriteFile(filepath.Join(data, "secrets", name), bytes.Repeat([]byte{0x37}, 32), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dependencies, err := filepath.Abs(filepath.Join("..", "..", "..", "data"))
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.Maintenance{
		DataDir: data, DBPath: path, DependencyRoot: dependencies,
		DependencyVersions: []string{"4.2.3"}, ActiveEJSVersion: "4.2.3",
	}
	service := maintenance.New(maintenancepersistence.New(), mediaFixtureNow)
	bundle, restored := filepath.Join(root, "bundle"), filepath.Join(root, "restored")
	if _, err := service.Backup(t.Context(), configuration, bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(t.Context(), configuration, bundle, restored); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(restored, "retrom.db")
}
