//go:build integration

package gamecontent

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/integration/payloadrelease"
	gamecontentmodel "retrom/internal/model/gamecontent"
	gamecontentservice "retrom/internal/service/gamecontent"
	"retrom/internal/service/uploads"
)

type failingPublicationRepository struct {
	*Repository
}

func (repository failingPublicationRepository) CommitPublish(
	ctx context.Context, cmd gamecontentmodel.PublishCommand,
) (gamecontentmodel.PublishResult, error) {
	result, err := repository.Repository.CommitPublish(ctx, cmd)
	if err != nil {
		return result, err
	}
	return gamecontentmodel.PublishResult{}, context.DeadlineExceeded
}

func assertLatePublicationRollback(t *testing.T, database *sql.DB, blobs *blobstore.Store, uploadService *uploads.Service,
	releases *payloadrelease.Service, gameID string, version int64, blobID, saveID string,
) {
	t.Helper()
	upload := completeUpload(t, t.Context(), database, uploadService, "rollback.gba", []byte("late failure content"))
	repo := New(database).WithGCStager(releases)
	repository := failingPublicationRepository{repo}
	service := gamecontentservice.New(repository, time.Now).WithBlobStore(blobs).WithPayloadRelease(releases).WithGCStager(releases)
	result, err := service.Schedule(t.Context(), gameID, upload, version)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, t.Context(), database, result.JobID, "FAILED")
	var currentVersion int64
	var currentBlob string
	var saves, successes int
	err = database.QueryRowContext(t.Context(), `SELECT g.version,f.blob_id,
 (SELECT count(*) FROM save_states WHERE id=?),
 (SELECT count(*) FROM job_events WHERE job_id=? AND event_type='SUCCEEDED')
 FROM games g JOIN game_files f ON f.game_id=g.id WHERE g.id=? ORDER BY f.sort_order LIMIT 1`,
		saveID, result.JobID, gameID).Scan(&currentVersion, &currentBlob, &saves, &successes)
	if err != nil {
		t.Fatal(err)
	}
	if currentVersion != version || currentBlob != blobID || saves != 1 || successes != 0 {
		t.Fatalf("partial replacement escaped rollback: version=%d blob=%s saves=%d successes=%d", currentVersion, currentBlob, saves, successes)
	}
}
