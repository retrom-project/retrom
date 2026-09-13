package uploads

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	jobpersistence "retrom/internal/persistence/jobs"
	jobservice "retrom/internal/service/jobs"
	uploadservice "retrom/internal/service/uploads"
)

func TestFinalizationManualRetryKeepsRoundAndCompletedFiles(t *testing.T) {
	fixture := newFinalizationFixture(t)
	session := uploadRetryFiles(t, fixture)
	fixture.service.Close()
	job := fixture.complete(t, session)
	var calls atomic.Int64
	failure := errors.New("temporary second-file CAS failure")
	worker := uploadservice.New(New(fixture.database), finalizationBlobs{put: func(reader io.Reader) (blobstore.Metadata, error) {
		if calls.Add(1) == 2 {
			return blobstore.Metadata{}, failure
		}
		return fixture.blobs.Put(reader)
	}}, fixture.root, finalizationNow)
	t.Cleanup(worker.Close)
	if err := worker.Run(t.Context(), job); !errors.Is(err, failure) {
		t.Fatalf("expected original CAS failure, got %v", err)
	}
	var completedID, completedBlob string
	if err := fixture.database.QueryRowContext(t.Context(), `
SELECT id,final_blob_id FROM upload_files WHERE upload_session_id=? AND state='COMPLETE'`, session.ID).
		Scan(&completedID, &completedBlob); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT version FROM jobs WHERE id=?`, job).Scan(&version); err != nil {
		t.Fatal(err)
	}
	result, err := jobservice.New(jobpersistence.New(fixture.database), finalizationNow).Retry(t.Context(), job, version)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExecutionNo != 2 || result.JobID != job {
		t.Fatalf("wrong retry: %+v", result)
	}
	if err := worker.Run(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	current, err := worker.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.FinalizationNo != 1 || current.State != "COMPLETE" || calls.Load() != 3 {
		t.Fatalf("retry round/calls: %+v %d", current, calls.Load())
	}
	var preserved int
	if err := fixture.database.QueryRowContext(t.Context(), `
SELECT count(*) FROM upload_files WHERE id=? AND final_blob_id=? AND state='COMPLETE'`, completedID, completedBlob).
		Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("completed file changed: %d %v", preserved, err)
	}
}

func uploadRetryFiles(t *testing.T, fixture *finalizationFixture) uploadservice.Session {
	t.Helper()
	session, err := fixture.service.Create(t.Context(), uploadservice.CreateRequest{
		SourceType: "FILES", Files: []uploadservice.FileDeclaration{
			{ClientFileID: "first", RelativePath: "first.bin", SizeBytes: 5},
			{ClientFileID: "second", RelativePath: "second.bin", SizeBytes: 5},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, file := range session.Files {
		fixture.put(t, uploadservice.Session{
			ID: session.ID, TotalBytes: file.SizeBytes,
			Files: []uploadservice.File{file},
		}, 0, []byte{1, 2, 3, 4, byte(index)})
	}
	current, err := fixture.service.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func TestFinalizationStartRecoversQueuedAndCancelledJobs(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "cancelled"}[cancelled], func(t *testing.T) {
			fixture := newFinalizationFixture(t)
			session := fixture.upload(t, []byte("bytes"))
			fixture.service.Close()
			job := fixture.complete(t, session)
			if cancelled {
				_, _, err := jobservice.New(jobpersistence.New(fixture.database), finalizationNow).Cancel(t.Context(), job, 1, "user cancelled")
				if err != nil {
					t.Fatal(err)
				}
			}
			restored := uploadservice.New(New(fixture.database), fixture.blobs, fixture.root, finalizationNow)
			t.Cleanup(restored.Close)
			restored.Start(t.Context())
			expected := "SUCCEEDED"
			if cancelled {
				expected = "CANCELLED"
			}
			awaitFinalizeState(t, fixture.database, job, expected)
			if cancelled {
				awaitUploadState(t, restored, session.ID, "CANCELLED")
			}
		})
	}
}

func awaitUploadState(t *testing.T, service *uploadservice.Service, id, state string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3e9)
	defer cancel()
	for {
		current, err := service.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == state {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("upload state=%s want=%s", current.State, state)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
