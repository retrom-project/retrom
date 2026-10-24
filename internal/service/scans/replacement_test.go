package scans

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"

	"github.com/google/uuid"
)

func replacementTestTask(ctx context.Context, gameID string) *replacementTask {
	worker, cancel := context.WithCancel(ctx)
	return &replacementTask{ctx: worker, cancel: cancel, session: replacementSession{Progress: model.ContentReplacement{
		ID: uuid.NewString(), GameID: gameID, Status: "queued", Phase: "queued", CanCancel: true,
	}}}
}

func replacementTestService(t *testing.T) *Service {
	t.Helper()
	return &Service{Redis: &memoryTemporary{}, Now: time.Now, Context: t.Context()}
}

func TestReplacementQueueIsBoundedIndependentAndQueuedCancellationReleasesCapacity(t *testing.T) {
	s := replacementTestService(t)
	active := replacementTestTask(t.Context(), uuid.NewString())
	defer active.cancel()
	s.replacements.active = active
	first := replacementTestTask(t.Context(), uuid.NewString())
	second := replacementTestTask(t.Context(), uuid.NewString())
	for _, task := range []*replacementTask{first, second} {
		if err := s.enqueueReplacement(t.Context(), task.ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	extra := replacementTestTask(t.Context(), uuid.NewString())
	defer extra.cancel()
	if err := s.enqueueReplacement(t.Context(), extra.ctx, extra); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("unbounded replacement admission: %v", err)
	}
	duplicate := replacementTestTask(t.Context(), first.session.Progress.GameID)
	defer duplicate.cancel()
	if err := s.enqueueReplacement(t.Context(), duplicate.ctx, duplicate); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same-game concurrent admission: %v", err)
	}
	progress, err := s.CancelReplacement(t.Context(), admin(), first.session.Progress.GameID, first.session.Progress.ID)
	if err != nil || progress.Status != "cancelled" || progress.CanCancel {
		t.Fatalf("queued cancellation waits for active work: %+v %v", progress, err)
	}
	if err = s.enqueueReplacement(t.Context(), extra.ctx, extra); err != nil {
		t.Fatalf("cancelled queue entry retained capacity: %v", err)
	}
	if len(s.replacements.queue) != 2 || s.replacements.queue[0] != second || s.replacements.queue[1] != extra {
		t.Fatal("replacement FIFO order changed")
	}
	for _, task := range []*replacementTask{second, extra} {
		if _, err = s.CancelReplacement(t.Context(), admin(), task.session.Progress.GameID, task.session.Progress.ID); err != nil {
			t.Fatal(err)
		}
	}
	s.Wait()
}

func activeReplacementTest(t *testing.T) (*Service, *replacementTask) {
	t.Helper()
	s := replacementTestService(t)
	task := replacementTestTask(t.Context(), uuid.NewString())
	t.Cleanup(task.cancel)
	task.session.Progress.Status = "running"
	s.replacements.active = task
	s.replacements.byGame = map[string]*replacementTask{task.session.Progress.GameID: task}
	return s, task
}

func TestReplacementQueueDoesNotConsumeScanCapacity(t *testing.T) {
	s, active := activeReplacementTest(t)
	for range 2 {
		_, cancel := context.WithCancel(t.Context())
		id := uuid.NewString()
		if err := s.admit(id, cancel); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.release(id); s.workers.Done() })
	}
	queued := replacementTestTask(t.Context(), uuid.NewString())
	if err := s.enqueueReplacement(t.Context(), queued.ctx, queued); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelReplacement(t.Context(), admin(), queued.session.Progress.GameID, queued.session.Progress.ID); err != nil {
		t.Fatal(err)
	}
	active.cancel()
}

func TestReplacementPublicationRefusesCancellation(t *testing.T) {
	s, task := activeReplacementTest(t)
	if err := s.beginReplacementPublication(t.Context(), task, "prepared-file", model.ContentReplacementResult{
		Version: 5, ContentHash: "new-hash",
	}); err != nil {
		t.Fatal(err)
	}
	progress, err := s.CancelReplacement(t.Context(), admin(), task.session.Progress.GameID, task.session.Progress.ID)
	if err != nil || progress.CanCancel || progress.Status != "running" || progress.Phase != "publishing" || task.ctx.Err() != nil {
		t.Fatalf("cancellation crossed publication boundary: %+v %v", progress, err)
	}
}

func TestReplacementCancellationPreventsPublication(t *testing.T) {
	s, task := activeReplacementTest(t)
	progress, err := s.CancelReplacement(t.Context(), admin(), task.session.Progress.GameID, task.session.Progress.ID)
	if err != nil || progress.CanCancel || progress.Status != "canceling" || !errors.Is(task.ctx.Err(), context.Canceled) {
		t.Fatalf("running preparation did not receive cancellation: %+v %v", progress, err)
	}
	if err = s.beginReplacementPublication(task.ctx, task, "file", model.ContentReplacementResult{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation started publication: %v", err)
	}
}

func TestReplacementProgressRestartExpiryAndAdminBoundary(t *testing.T) {
	s := replacementTestService(t)
	task := replacementTestTask(t.Context(), uuid.NewString())
	defer task.cancel()
	if err := s.saveReplacement(t.Context(), &task.session); err != nil {
		t.Fatal(err)
	}
	progress, err := s.ContentReplacement(t.Context(), admin(), task.session.Progress.GameID, "")
	if err != nil || progress.Status != "interrupted" || progress.CanCancel || progress.Error == nil {
		t.Fatalf("orphan progress remained pending after restart: %+v %v", progress, err)
	}
	for _, id := range []string{"", progress.ID} {
		current, readErr := s.ContentReplacement(t.Context(), admin(), progress.GameID, id)
		if readErr != nil || current.Status != "interrupted" {
			t.Fatalf("terminal restart result was not retained: %+v %v", current, readErr)
		}
	}
	if _, err = s.ContentReplacement(t.Context(), admin(), progress.GameID, uuid.NewString()); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("old operation ID resolved to another operation: %v", err)
	}
	if err = s.Redis.Delete(t.Context(), replacementKey(progress.GameID)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ContentReplacement(t.Context(), admin(), progress.GameID, progress.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expired operation did not return unavailable: %v", err)
	}
	player := admin()
	player.User.Role = "player"
	if _, err = s.ContentReplacement(t.Context(), player, progress.GameID, ""); err == nil {
		t.Fatal("player read administrator operation")
	}
	if _, err = s.CancelReplacement(t.Context(), player, progress.GameID, progress.ID); err == nil {
		t.Fatal("player cancelled administrator operation")
	}
}

func TestReplacementShutdownCancelsQueuedOperationsWithoutWaitingForActive(t *testing.T) {
	serviceContext, stop := context.WithCancel(t.Context())
	s := replacementTestService(t)
	s.Context = serviceContext
	s.replacements.active = replacementTestTask(t.Context(), uuid.NewString())
	defer s.replacements.active.cancel()
	task := replacementTestTask(serviceContext, uuid.NewString())
	if err := s.enqueueReplacement(t.Context(), task.ctx, task); err != nil {
		t.Fatal(err)
	}
	stop()
	s.Wait()
	progress, err := s.ContentReplacement(t.Context(), admin(), task.session.Progress.GameID, task.session.Progress.ID)
	if err != nil || progress.Status != "interrupted" || progress.CanCancel {
		t.Fatalf("shutdown retained queued work: %+v %v", progress, err)
	}
	rejected := replacementTestTask(t.Context(), uuid.NewString())
	defer rejected.cancel()
	if err = s.enqueueReplacement(t.Context(), rejected.ctx, rejected); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("shutdown accepted replacement: %v", err)
	}
}

func TestReplacementFailuresExposeSafeSpecificResults(t *testing.T) {
	for _, test := range []struct {
		err    error
		status string
		code   string
	}{
		{context.Canceled, "cancelled", ""},
		{context.DeadlineExceeded, "failed", "CONTENT_REPLACEMENT_TIMEOUT"},
		{model.ErrConflict, "failed", "VERSION_CONFLICT"},
		{model.ErrStorageFull, "failed", "INSUFFICIENT_STORAGE"},
		{fmt.Errorf("runtime: %w (RUNTIME_BUSY)", model.ErrUnavailable), "failed", "RUNTIME_BUSY"},
		{persistence.ErrCommitUncertain, "interrupted", "CONTENT_REPLACEMENT_COMMIT_UNCERTAIN"},
		{errors.New("/private/mounted/source/path invalid"), "failed", "CONTENT_REPLACEMENT_FAILED"},
	} {
		s := replacementTestService(t)
		task := replacementTestTask(t.Context(), uuid.NewString())
		s.replacementFailure(task, test.err)
		task.cancel()
		progress := task.session.Progress
		if progress.Status != test.status || test.code != "" && (progress.Error == nil || progress.Error.Code != test.code) {
			t.Fatalf("failure %v became %+v", test.err, progress)
		}
		if progress.Error != nil && strings.Contains(progress.Error.Message, "/private") {
			t.Fatal("operation leaked a source path")
		}
	}
}
