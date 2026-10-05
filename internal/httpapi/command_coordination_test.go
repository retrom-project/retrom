package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	dbapi "retrom/internal/database"
	"retrom/internal/database/postgres"
	idempotencypersistence "retrom/internal/persistence/idempotency"
	"retrom/internal/service/idempotency"
	"retrom/internal/store"
	"retrom/internal/testsupport/testpostgres"
)

type commandPeers struct {
	database dbapi.DB
	servers  [2]*Server
	calls    atomic.Int64
	before   func(context.Context)
	after    func()
}

func newCommandPeers(t *testing.T) *commandPeers {
	t.Helper()
	dsn := testpostgres.DSN(t)
	base, err := store.Open(t.Context(), dsn, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := base.Close(); err != nil {
			t.Error(err)
		}
	})
	peer, err := postgres.Open(dsn, postgres.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := peer.Close(); err != nil {
			t.Error(err)
		}
	})
	base.SQL.SetMaxOpenConns(1)
	fixture := &commandPeers{database: base.SQL}
	for index, database := range []dbapi.DB{base.SQL, peer} {
		fixture.servers[index] = &Server{now: time.Now, systemDeps: SystemDependencies{
			Idempotency: idempotency.New(idempotencypersistence.New(database)),
		}}
	}
	return fixture
}

func (fixture *commandPeers) handler(index int) http.Handler {
	server := fixture.servers[index]
	return server.idempotencyHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture.calls.Add(1)
		ctx := request.Context()
		if fixture.before != nil {
			fixture.before(ctx)
		}
		id := uuid.NewString()
		err := dbapi.RetryTransaction(ctx, fixture.database, func(tx dbapi.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id,actor_kind,actor_label,action,
resource_type,resource_id,created_at_ms) VALUES(?,'SYSTEM','startup-test-bootstrap','PROBE','FIXTURE',?,0)`, id, id); err != nil {
				return err
			}
			return idempotency.Complete(ctx, idempotency.Result{Value: map[string]string{"id": id}, Version: 1, ResourceID: id})
		})
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		writeJSON(writer, http.StatusCreated, map[string]string{"id": id})
		if fixture.after != nil {
			afterIdempotencyCommit(writer, fixture.after)
		}
	}))
}

func (fixture *commandPeers) send(ctx context.Context, index int, key string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	fixture.handler(index).ServeHTTP(recorder, sourceCommandRequest(ctx, "/tags", "postAdminTag", key))
	return recorder
}

func awaitCommandSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("command coordination timed out")
	}
}

func assertCommandReplay(t *testing.T, original, replay *httptest.ResponseRecorder) {
	t.Helper()
	if original.Code != 201 || replay.Code != 201 || original.Body.String() != replay.Body.String() {
		t.Fatalf("original/replay: %d/%d %s/%s", original.Code, replay.Code, original.Body.String(), replay.Body.String())
	}
	for _, name := range []string{"ETag", "Location", "Content-Type"} {
		if original.Header().Get(name) == "" || original.Header().Get(name) != replay.Header().Get(name) {
			t.Fatalf("replay lost %s", name)
		}
	}
	if replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" {
		t.Fatal("replay header missing")
	}
}

func TestCommandCoordinationAcrossPoolsCommitsOneResult(t *testing.T) {
	fixture := newCommandPeers(t)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	var once sync.Once
	fixture.before = func(context.Context) { once.Do(func() { close(entered) }); <-release }
	key := uuid.NewString()
	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- fixture.send(t.Context(), 0, key) }()
	awaitCommandSignal(t, entered)
	if stats := fixture.database.Stats(); stats.InUse != 0 {
		t.Fatalf("slow preparation held writer connection: %d", stats.InUse)
	}
	go func() { responses <- fixture.send(t.Context(), 1, key) }()
	close(release)
	first, second := <-responses, <-responses
	if first.Header().Get("X-Retrom-Idempotent-Replay") == "true" {
		first, second = second, first
	}
	assertCommandReplay(t, first, second)
	if fixture.calls.Load() != 1 {
		t.Fatalf("executed %d duplicate commands", fixture.calls.Load())
	}
	conflicting := sourceCommandRequest(t.Context(), "/tags", "postAdminTag", key)
	conflicting.Body = http.NoBody
	response := httptest.NewRecorder()
	fixture.handler(1).ServeHTTP(response, conflicting)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("conflict: %d/%s", response.Code, response.Body.String())
	}
}

func TestCommandCancellationDoesNotLeakCrossPoolIdentityLock(t *testing.T) {
	fixture := newCommandPeers(t)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	var once sync.Once
	fixture.before = func(context.Context) { once.Do(func() { close(entered) }); <-release }
	key := uuid.NewString()
	original := make(chan *httptest.ResponseRecorder, 1)
	go func() { original <- fixture.send(t.Context(), 0, key) }()
	awaitCommandSignal(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	cancelled := fixture.send(ctx, 1, key)
	if cancelled.Body.Len() != 0 || ctx.Err() == nil || fixture.calls.Load() != 1 {
		t.Fatalf("cancelled wait entered handler: %d/%d", cancelled.Code, fixture.calls.Load())
	}
	close(release)
	first := <-original
	replayCtx, replayCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer replayCancel()
	assertCommandReplay(t, first, fixture.send(replayCtx, 1, key))
}

func TestCommandAfterCommitWorkDoesNotHoldIdentityLock(t *testing.T) {
	fixture := newCommandPeers(t)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	fixture.after = func() { close(entered); <-release }
	key := uuid.NewString()
	original := make(chan *httptest.ResponseRecorder, 1)
	go func() { original <- fixture.send(t.Context(), 0, key) }()
	awaitCommandSignal(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	replay := fixture.send(ctx, 1, key)
	close(release)
	assertCommandReplay(t, <-original, replay)
	if fixture.calls.Load() != 1 {
		t.Fatal("replay executed after-commit work")
	}
}
