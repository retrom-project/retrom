//go:build integration

package gamecontent

import (
	"context"
	"testing"
	"time"

	"retrom/internal/composition/cleanupjobs"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/service/gamecontent"
	"retrom/internal/service/uploads"
)

type failingPublicationRepository struct{ gamecontent.Repository }

func (repository failingPublicationRepository) WithWrite(ctx context.Context,
	work func(gamecontent.WriteScope) error,
) error {
	return repository.Repository.WithWrite(ctx, func(scope gamecontent.WriteScope) error {
		scope.ContentWriter = failingPublicationWriter{scope.ContentWriter}
		return work(scope)
	})
}

type failingPublicationWriter struct{ gamecontent.ContentWriter }

func (writer failingPublicationWriter) Publish(ctx context.Context, value gamecontent.Publication) error {
	if err := writer.ContentWriter.Publish(ctx, value); err != nil {
		return err
	}
	return context.DeadlineExceeded
}

func assertLatePublicationRollback(t *testing.T, database dbapi.DB, blobs *filestore.Store,
	uploadService *uploads.Service,
	releases *cleanupjobs.Service, gameID string, version int64, fileRecord, saveID string,
) {
	t.Helper()
	upload := completeUpload(t, t.Context(), database, uploadService, "rollback.gba", []byte("late failure content"))
	repository := failingPublicationRepository{New(database)}
	service := gamecontent.New(gamecontent.Dependencies{Repository: repository, Files: blobs, Cleanup: releases}, gamecontent.Options{Now: time.Now})
	result, err := service.Schedule(t.Context(), gameID, upload, version)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, t.Context(), database, result.JobID, "FAILED")
	var currentVersion int64
	var currentBlob string
	var saves, successes int
	err = dbapi.QueryRowContext(t.Context(), database, `SELECT g.version,f.file_record,
 (SELECT count(*) FROM save_states WHERE id=?),
 (SELECT count(*) FROM job_events WHERE job_id=? AND event_type='SUCCEEDED')
 FROM games g JOIN game_files f ON f.game_id=g.id WHERE g.id=? ORDER BY f.sort_order LIMIT 1`,
		saveID, result.JobID, gameID).Scan(&currentVersion, &currentBlob, &saves, &successes)
	if err != nil {
		t.Fatal(err)
	}
	if currentVersion != version || currentBlob != fileRecord || saves != 1 || successes != 0 {
		t.Fatalf("partial replacement escaped rollback: version=%d blob=%s saves=%d successes=%d",
			currentVersion, currentBlob, saves, successes)
	}
}
