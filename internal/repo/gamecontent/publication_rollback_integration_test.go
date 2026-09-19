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
	"retrom/internal/service/gamecontent"
	"retrom/internal/service/uploads"
)

type failingPublicationRepository struct{ gamecontentmodel.Repository }

func (repository failingPublicationRepository) WithWrite(ctx context.Context, work func(gamecontentmodel.WriteScope) error) error {
	return repository.Repository.WithWrite(ctx, func(scope gamecontentmodel.WriteScope) error {
		scope.ContentWriter = failingPublicationWriter{scope.ContentWriter}
		return work(scope)
	})
}

type failingPublicationWriter struct{ gamecontentmodel.ContentWriter }

func (writer failingPublicationWriter) Publish(ctx context.Context, value gamecontentmodel.Publication) error {
	if err := writer.ContentWriter.Publish(ctx, value); err != nil {
		return err
	}
	return context.DeadlineExceeded
}

func assertLatePublicationRollback(t *testing.T, database *sql.DB, blobs *blobstore.Store, uploadService *uploads.Service,
	releases *payloadrelease.Service, gameID string, version int64, blobID, saveID string,
) {
	t.Helper()
	upload := completeUpload(t, t.Context(), database, uploadService, "rollback.gba", []byte("late failure content"))
	repository := failingPublicationRepository{New(database)}
	service := gamecontent.New(repository, time.Now).WithBlobStore(blobs).WithPayloadRelease(releases).WithGCStager(releases)
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
