//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	dependencypersistence "retrom/internal/persistence/dependencies"
	variantrepository "retrom/internal/persistence/gamevariant"
	dependencyservice "retrom/internal/service/dependencies"
	"retrom/internal/service/gamevariant"
)

func TestDistinctMoveCommandsShareConcurrentVariantAdmission(t *testing.T) {
	server := newTestServer(t)
	if err := dependencyservice.New(server.dependencies, dependencypersistence.New(server.database)).Bootstrap(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(server.dependencies, dependencypersistence.New(server.database)).BootstrapCatalogs(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	gameID, _ := seedMovableGame(t, server)
	type outcome struct {
		result gamevariant.Result
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, err := server.playDeps.Variants.Ensure(t.Context(), gameID, "mgba")
			results <- outcome{result, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent admission: %v / %v", first.err, second.err)
	}
	if first.result.JobID == "" || first.result.JobID != second.result.JobID {
		t.Fatalf("admission created distinct jobs: %#v / %#v", first.result, second.result)
	}
}

// Both write snapshots observe no target variant before either allocates it.
// This catches the creation race that a global HTTP mutex used to hide.
func TestMoveCommandsRecheckSimultaneousMissingVariantSnapshots(t *testing.T) {
	testSimultaneousVariantAdmission(t, "mgba", false)
}

func TestMoveCommandsRecheckSimultaneousMissingJobSnapshots(t *testing.T) {
	testSimultaneousVariantAdmission(t, "gambatte", true)
}

func testSimultaneousVariantAdmission(t *testing.T, coreID string, jobsBarrier bool) {
	t.Helper()
	server := newTestServer(t)
	dependencies := dependencyservice.New(server.dependencies, dependencypersistence.New(server.database))
	if err := dependencies.Bootstrap(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := dependencies.BootstrapCatalogs(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	gameID, _ := seedMovableGame(t, server)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	barrier := &variantReadBarrier{
		Repository: variantrepository.New(server.database), ready: make(chan struct{}), jobs: jobsBarrier,
	}
	variants := gamevariant.New(barrier, server.launchSources, time.Now, nil, nil)
	type outcome struct {
		result gamevariant.Result
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, err := variants.Ensure(ctx, gameID, coreID)
			results <- outcome{result, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent admission: %v / %v", first.err, second.err)
	}
	if first.result.JobID == "" || first.result.JobID != second.result.JobID {
		t.Fatalf("admission created distinct jobs: %#v / %#v", first.result, second.result)
	}
	var count, jobs int
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT count(*),
(SELECT count(*) FROM jobs WHERE kind='VARIANT_VALIDATE' AND scope_id IN
 (SELECT id FROM game_variants WHERE game_id=? AND core_id=?))
FROM game_variants WHERE game_id=? AND core_id=?`, gameID, coreID, gameID, coreID).Scan(&count, &jobs); err != nil {
		t.Fatal(err)
	}
	if count != 1 || jobs != 1 {
		t.Fatalf("concurrent variants=%d jobs=%d", count, jobs)
	}
}

type variantReadBarrier struct {
	gamevariant.Repository
	reads atomic.Int64
	ready chan struct{}
	jobs  bool
}

func (repository *variantReadBarrier) WithEnsure(ctx context.Context, work func(gamevariant.EnsureScope) error) error {
	return repository.Repository.WithEnsure(ctx, func(scope gamevariant.EnsureScope) error {
		original := scope.Read
		scope.Read = func(ctx context.Context, gameID, coreID string) (gamevariant.Snapshot, error) {
			snapshot, err := original(ctx, gameID, coreID)
			if !repository.jobs {
				if waitErr := repository.wait(ctx); waitErr != nil {
					return gamevariant.Snapshot{}, waitErr
				}
			}
			return snapshot, err
		}
		if repository.jobs {
			scope.Write = variantJobReadBarrier{scope.Write, repository}
		}
		return work(scope)
	})
}

func (repository *variantReadBarrier) wait(ctx context.Context) error {
	read := repository.reads.Add(1)
	if read == 2 {
		close(repository.ready)
	}
	if read <= 2 {
		select {
		case <-repository.ready:
		case <-ctx.Done():
			return fmt.Errorf("variant barrier: %w", ctx.Err())
		}
	}
	return nil
}

type variantJobReadBarrier struct {
	gamevariant.WriteScope
	barrier *variantReadBarrier
}

func (scope variantJobReadBarrier) Find(ctx context.Context, dedupe string) (gamevariant.ValidationJob, bool, error) {
	job, found, err := scope.WriteScope.Find(ctx, dedupe)
	if waitErr := scope.barrier.wait(ctx); waitErr != nil {
		return gamevariant.ValidationJob{}, false, waitErr
	}
	return job, found, err
}
