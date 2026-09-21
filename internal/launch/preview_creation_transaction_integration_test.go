//go:build integration

package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"retrom/internal/cleanup"
	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"

	"github.com/google/uuid"
	"modernc.org/sqlite"
)

type previewWriteFaultRepository struct {
	application.PreviewCreationRepository
	change  func(*application.PreviewCreatePlan)
	after   func() error
	reached bool
}

func (repository *previewWriteFaultRepository) WithCreation(ctx context.Context, work func(application.PreviewCreationScope) error) error {
	return repository.PreviewCreationRepository.WithCreation(ctx, func(scope application.PreviewCreationScope) error {
		err := work(previewWriteFaultScope{PreviewCreationScope: scope, change: repository.change})
		if err != nil {
			return err
		}
		repository.reached = true
		if repository.after != nil {
			return repository.after()
		}
		return nil
	})
}

type previewWriteFaultScope struct {
	application.PreviewCreationScope
	change func(*application.PreviewCreatePlan)
}

func (scope previewWriteFaultScope) Create(ctx context.Context, plan application.PreviewCreatePlan) error {
	if scope.change != nil {
		scope.change(&plan)
	}
	return scope.PreviewCreationScope.Create(ctx, plan)
}

func previewCreationRows(t *testing.T, database *sql.DB) map[string]string {
	t.Helper()
	result := playRows(t, database)
	rows, err := database.QueryContext(t.Context(), `SELECT preview_session_id,role,logical_name,blob_id,sort_order,COALESCE(virtual_path,'') FROM review_preview_files ORDER BY preview_session_id,role,logical_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close preview snapshot", rows.Close()) }()
	var files [][]any
	for rows.Next() {
		var id, role, name, blob, path string
		var order int64
		if err := rows.Scan(&id, &role, &name, &blob, &order, &path); err != nil {
			t.Fatal(err)
		}
		files = append(files, []any{id, role, name, blob, order, path})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	result["review_preview_files"] = string(encoded)
	return result
}

func assertPreviewCreationRollback(t *testing.T, service *Service, request ReviewPreviewRequest) {
	t.Helper()
	cause := errors.New("creation transaction interrupted after all owners")
	repository := &previewWriteFaultRepository{PreviewCreationRepository: persistence.NewPreviewCreation(service.database), after: func() error { return cause }}
	before := previewCreationRows(t, service.database)
	result, err := service.previewCreator(repository).Create(t.Context(), request)
	if !errors.Is(err, cause) || result != (ReviewPreviewCreated{}) || !repository.reached {
		t.Fatalf("post-write failure returned success: id=%q reached=%v error=%v", result.PreviewID, repository.reached, err)
	}
	if !reflect.DeepEqual(before, previewCreationRows(t, service.database)) {
		t.Fatal("post-write rollback left preview owners/files/tickets")
	}
	repository.after = nil
	repository.change = func(plan *application.PreviewCreatePlan) {
		plan.Content.Files = append(plan.Content.Files, application.PreviewFile{Role: "PROJECT_FILE", LogicalName: "late-failure.bin", BlobID: "does-not-exist"})
	}
	result, err = service.previewCreator(repository).Create(t.Context(), request)
	var storage *sqlite.Error
	if !errors.As(err, &storage) || result != (ReviewPreviewCreated{}) {
		t.Fatalf("late file failure lost cause: id=%q error=%v", result.PreviewID, err)
	}
	if !reflect.DeepEqual(before, previewCreationRows(t, service.database)) {
		t.Fatal("failed file write retained owners")
	}
}

func TestPreviewCreationRollsBackEveryProductIndependentOwner(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	assertPreviewCreationRollback(t, fixture.launcher, ReviewPreviewRequest{ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "rollback"})
}

func TestPreviewCreationRejectsEntropyFailureBeforeWrites(t *testing.T) {
	fixture := newReviewCheckpointFixture(t)
	before := previewCreationRows(t, fixture.database)
	cause := errors.New("preview identity entropy unavailable")
	result, err := func() (ReviewPreviewCreated, error) {
		uuid.SetRand(playEntropyFailure{cause: cause})
		defer uuid.SetRand(nil)
		return fixture.launcher.CreateReviewPreview(t.Context(), ReviewPreviewRequest{ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "entropy"})
	}()
	if !errors.Is(err, cause) || result != (ReviewPreviewCreated{}) {
		t.Fatalf("unchecked preview identity: id=%q error=%v", result.PreviewID, err)
	}
	if !reflect.DeepEqual(before, previewCreationRows(t, fixture.database)) {
		t.Fatal("entropy failure wrote a preview")
	}
}

func TestPreviewCreationReplaysTwoSimultaneousProductRequests(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	request := ReviewPreviewRequest{ImportItemID: fixture.itemID, ActorUserID: "reviewer", IdempotencyKey: "simultaneous"}
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	releaseBoth := sync.OnceFunc(func() { close(release) })
	defer releaseBoth()
	type outcome struct {
		result ReviewPreviewCreated
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		service := *fixture.launcher
		clock := service.now
		first := true
		service.now = func() time.Time {
			if first {
				first = false
				ready <- struct{}{}
				<-release
			}
			return clock()
		}
		go func() {
			result, err := service.CreateReviewPreview(t.Context(), request)
			results <- outcome{result, err}
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case early := <-results:
			t.Fatalf("creation failed before interleave: %v", early.err)
		case <-time.After(5 * time.Second):
			t.Fatal("creation did not reach interleave")
		}
	}
	releaseBoth()
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.result.PreviewID == "" || first.result != second.result {
		t.Fatalf("concurrent creation: first=%q second=%q errors=%v / %v", first.result.PreviewID, second.result.PreviewID, first.err, second.err)
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM review_preview_sessions WHERE actor_user_id=? AND idempotency_key=?`, request.ActorUserID, request.IdempotencyKey).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent request created %d owners", count)
	}
}
