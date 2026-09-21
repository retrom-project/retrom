package uploads_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	uploadpersistence "retrom/internal/persistence/uploads"
	"retrom/internal/service/uploads"
)

func TestConcurrentPartReplaysCountBytesOnce(t *testing.T) {
	service, database, session := partFixture(t, true)
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			failures <- service.PutPart(t.Context(), session.ID, session.Files[0].ID, 0,
				"bytes 0-4/5", digest([]byte("bytes")), bytes.NewReader([]byte("bytes")))
		})
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 2 || current.Files[0].Received != 5 || len(current.Files[0].Parts) != 1 {
		t.Fatalf("replays advanced state more than once: %+v", current)
	}
	var parts int
	if err := database.QueryRowContext(t.Context(), "SELECT count(*) FROM upload_parts").Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if parts != 1 {
		t.Fatalf("replays created %d parts", parts)
	}
}

func TestPartRechecksCancellationAfterStaging(t *testing.T) {
	service, database, session := partFixture(t, true)
	gate := &partCommitGate{Repository: uploadpersistence.New(database), staged: make(chan struct{}), proceed: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.proceed) }) }
	defer unblock()
	uploader := uploads.New(gate, nil, t.TempDir(), func() time.Time { return time.UnixMilli(1000) })
	result := make(chan error, 1)
	go func() {
		result <- uploader.PutPart(t.Context(), session.ID, session.Files[0].ID, 0,
			"bytes 0-4/5", digest([]byte("bytes")), bytes.NewReader([]byte("bytes")))
	}()
	select {
	case <-gate.staged:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if _, _, err := service.Cancel(t.Context(), session.ID, session.Version); err != nil {
		t.Fatal(err)
	}
	unblock()
	if err := <-result; !errors.Is(err, uploads.ErrInvalid) {
		t.Fatalf("late part accepted after cancellation: %v", err)
	}
	assertNoPartProgress(t, database, session, "CANCELLED")
}

type partCommitGate struct {
	*uploadpersistence.Repository
	staged, proceed chan struct{}
}

func (gate *partCommitGate) WithWrite(ctx context.Context, work func(uploads.WriteScope) error) error {
	close(gate.staged)
	select {
	case <-gate.proceed:
		return gate.Repository.WithWrite(ctx, work)
	case <-ctx.Done():
		return ctx.Err()
	}
}
