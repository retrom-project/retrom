//go:build integration

package scans

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func TestReplacementPreparationIsAsyncReadOnlyAndPublishesOnce(t *testing.T) {
	s, f, source, originals := normalizedSourceFixture(t, false)
	s.Redis, s.Context = &memoryTemporary{}, t.Context()
	f.Publish(t)
	before, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	if err != nil {
		t.Fatal(err)
	}
	request, stopRequest := context.WithCancel(t.Context())
	accepted, err := s.Replace(request, f.Principal, f.Game.ID, filepath.Join(source, "fixture.exe"), before.Game.Version)
	stopRequest()
	if err != nil || !model.UUID(accepted.ID) || accepted.GameID != f.Game.ID {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	s.Wait()
	progress, err := s.ContentReplacement(t.Context(), f.Principal, f.Game.ID, accepted.ID)
	assertReplacementCompleted(t, progress, err)
	after, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	assertReplacementPublication(t, before, after, progress, err)
	logical, err := f.Repository.ContentFiles(t.Context(), after.Files)
	if err != nil || len(logical) != 2 {
		t.Fatalf("normalized logical files=%+v err=%v", logical, err)
	}
	for _, file := range logical {
		if file.LogicalKey == "fixture.exe" {
			t.Fatal("original container survived in the published replacement")
		}
	}
	assertSourceUnchanged(t, source, originals)
	assertNoStaging(t, s)
	assertReplacementRecovery(t, s, f, progress, after)
	assertFailedReplacementKeepsContent(t, s, f, progress, after, source)
	assertSourceUnchanged(t, source, originals)
	assertNoStaging(t, s)
}

func TestReplacementQueueRechecksVersionBeforeReadingSource(t *testing.T) {
	s, f, source, originals := normalizedSourceFixture(t, false)
	s.Redis, s.Context = &memoryTemporary{}, t.Context()
	f.Publish(t)
	detail, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	if err != nil {
		t.Fatal(err)
	}
	active := replacementTestTask(t.Context(), uuid.NewString())
	defer active.cancel()
	s.replacements.active = active
	accepted, err := s.Replace(t.Context(), f.Principal, f.Game.ID, filepath.Join(source, "missing.exe"), detail.Game.Version)
	if err != nil || accepted.Status != "queued" {
		t.Fatalf("queued replacement=%+v err=%v", accepted, err)
	}
	if err = f.Repository.Transaction(t.Context(), func(r *persistence.Repository) error {
		return r.BumpGame(t.Context(), f.Game.ID, detail.Game.Version, 3000)
	}); err != nil {
		t.Fatal(err)
	}
	s.replacements.mutex.Lock()
	task := s.replacements.queue[0]
	s.replacements.queue = nil
	s.replacements.active = task
	go s.runReplacement(task)
	s.replacements.mutex.Unlock()
	s.Wait()
	progress, err := s.ContentReplacement(t.Context(), f.Principal, f.Game.ID, accepted.ID)
	if err != nil || progress.Status != "failed" || progress.Error == nil || progress.Error.Code != "VERSION_CONFLICT" {
		t.Fatalf("queued stale version read nonexistent source or replaced newer content: %+v err=%v", progress, err)
	}
	current, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	if err != nil || current.Files[0].ID != detail.Files[0].ID || current.Game.Version != detail.Game.Version+1 {
		t.Fatal("stale replacement changed the active content")
	}
	assertSourceUnchanged(t, source, originals)
	assertNoStaging(t, s)
}

func assertReplacementCompleted(t *testing.T, progress model.ContentReplacement, err error) {
	t.Helper()
	if err != nil || progress.Status != "completed" || progress.CanCancel || progress.Result == nil {
		t.Fatalf("request completion cancelled asynchronous preparation: %+v err=%v", progress, err)
	}
}

func assertReplacementPublication(t *testing.T, before, after model.GameDetail, progress model.ContentReplacement, err error) {
	t.Helper()
	if err != nil || after.Game.Version != before.Game.Version+1 || after.Game.ContentHash != progress.Result.ContentHash ||
		progress.Result.Version != after.Game.Version || len(after.Files) != 1 || after.Files[0].Role != "project_package" {
		t.Fatalf("publication=%+v progress=%+v err=%v", after, progress, err)
	}
}

func assertReplacementRecovery(t *testing.T, s *Service, f testsupport.Fixture, progress model.ContentReplacement,
	after model.GameDetail,
) {
	t.Helper()
	// A process can die after DB commit but before final Redis progress. Recovery
	// checks this operation's file ID, and does not publish a second version.
	session := replacementSession{Progress: progress, FileID: after.Files[0].ID, Prepared: progress.Result}
	session.Progress.Status, session.Progress.Phase = "running", "publishing"
	session.Progress.Result = nil
	if err := s.saveReplacement(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	restarted := &Service{Repository: f.Repository, Redis: s.Redis, Now: s.Now, Context: t.Context()}
	recovered, err := restarted.ContentReplacement(t.Context(), f.Principal, f.Game.ID, progress.ID)
	if err != nil || recovered.Status != "completed" || recovered.Result == nil || recovered.Result.Version != after.Game.Version {
		t.Fatalf("committed orphan was misreported: %+v err=%v", recovered, err)
	}
	// Re-check a previously uncertain commit when the database becomes available.
	session.Progress.Status = "interrupted"
	session.Progress.Error = replacementError("CONTENT_REPLACEMENT_COMMIT_UNCERTAIN", "Pending verification")
	if err := s.saveReplacement(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	recovered, err = restarted.ContentReplacement(t.Context(), f.Principal, f.Game.ID, progress.ID)
	if err != nil || recovered.Status != "completed" {
		t.Fatalf("uncertain commit was never checked again: %+v err=%v", recovered, err)
	}
}

func assertFailedReplacementKeepsContent(t *testing.T, s *Service, f testsupport.Fixture,
	previous model.ContentReplacement, after model.GameDetail, source string,
) {
	t.Helper()
	// A failed later preparation replaces only temporary progress, not active files.
	failed, err := s.Replace(t.Context(), f.Principal, f.Game.ID, filepath.Join(source, "missing.exe"), after.Game.Version)
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	progress, err := s.ContentReplacement(t.Context(), f.Principal, f.Game.ID, failed.ID)
	if err != nil || progress.Status != "failed" {
		t.Fatalf("missing source=%+v err=%v", progress, err)
	}
	current, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, f.Game.ID, "published")
	if err != nil || current.Game.Version != after.Game.Version || current.Files[0].ID != after.Files[0].ID {
		t.Fatal("failed replacement changed active content")
	}
	if _, err = s.ContentReplacement(t.Context(), f.Principal, f.Game.ID, previous.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("old operation ID read the later operation: %v", err)
	}
}
