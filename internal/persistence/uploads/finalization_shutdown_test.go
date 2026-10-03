package uploads

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"retrom/internal/filestore"
	jobpersistence "retrom/internal/persistence/jobs"
	jobservice "retrom/internal/service/jobs"
	uploadservice "retrom/internal/service/uploads"
)

func TestFinalizationCloseCancelsReadingAndJoinsCleanup(t *testing.T) {
	fixture := newFinalizationFixture(t)
	entered, read := make(chan struct{}), make(chan error, 1)
	fixture.service = uploadservice.New(New(fixture.database),
		finalizationBlobs{store: fixture.blobs, put: func(reader io.Reader) (filestore.Metadata, error) {
			close(entered)
			for {
				_, err := reader.Read(make([]byte, 0))
				if err != nil {
					read <- err
					return filestore.Metadata{}, err
				}
				time.Sleep(time.Millisecond)
			}
		}}, fixture.root, finalizationNow)
	job := fixture.complete(t, fixture.upload(t, []byte("bytes")))
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reader not reached")
	}
	fixture.service.Close()
	if err := <-read; !errors.Is(err, context.Canceled) {
		t.Fatalf("read survived close: %v", err)
	}
	if fixture.service.Resume(t.Context(), job) {
		t.Fatal("worker registered after Close")
	}
	awaitFinalizeState(t, fixture.database, job, "FAILED")
}

func TestFinalizationPersistentCancelStopsSingleFileRead(t *testing.T) {
	fixture := newFinalizationFixture(t)
	entered := make(chan struct{})
	fixture.service = uploadservice.New(New(fixture.database),
		finalizationBlobs{store: fixture.blobs, put: func(reader io.Reader) (filestore.Metadata, error) {
			close(entered)
			for {
				_, err := reader.Read(make([]byte, 0))
				if err != nil {
					return filestore.Metadata{}, err
				}
				time.Sleep(time.Millisecond)
			}
		}}, fixture.root, finalizationNow)
	session := fixture.upload(t, []byte("bytes"))
	job := fixture.complete(t, session)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reader not reached")
	}
	_, pending, err := jobservice.New(jobpersistence.New(fixture.database),
		finalizationNow).Cancel(t.Context(), job, "stop read")
	if err != nil || !pending {
		t.Fatalf("cancel: %v %v", pending, err)
	}
	awaitFinalizeState(t, fixture.database, job, "CANCELLED")
	awaitUploadState(t, fixture.service, session.ID, "CANCELLED")
	fixture.service.Close()
}
