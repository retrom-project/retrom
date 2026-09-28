//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"retrom/internal/testsupport/importfixture"

	variantcomposition "retrom/internal/composition/gamevariant"
	variantrepository "retrom/internal/persistence/gamevariant"
	gamevariant "retrom/internal/service/gamevariant"

	"retrom/internal/cleanup"
	launchcomposition "retrom/internal/composition/launch"
	dbapi "retrom/internal/database"
	"retrom/internal/dependencies"
	"retrom/internal/launch"
	dependencypersistence "retrom/internal/persistence/dependencies"
	idempotencypersistence "retrom/internal/persistence/idempotency"
	jobpersistence "retrom/internal/persistence/jobs"
	dependencyservice "retrom/internal/service/dependencies"
	idempotencyservice "retrom/internal/service/idempotency"
	"retrom/internal/service/jobs"
	"retrom/internal/testsupport"
)

const validationRetryKey = "01980000-0000-7000-8000-000000000097"

type validationRetryFixture struct {
	server *testServer
	jobID  string
	now    func() time.Time
}

func newValidationRetryFixture(t *testing.T) validationRetryFixture {
	t.Helper()
	now := func() time.Time { return time.UnixMilli(1_786_000_000_000) }
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close validation retry", database.Close()) })
	catalog, err := dependencies.Load(filepath.Join("..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(catalog,
		dependencypersistence.New(database.SQL)).Bootstrap(t.Context(), now()); err != nil {
		t.Fatal(err)
	}
	variants := variantcomposition.New(database.SQL, launch.NewSources(nil, nil), now)
	server := &testServer{
		database: database.SQL,
		Server: &Server{
			systemDeps: SystemDependencies{
				Idempotency: idempotencyservice.New(idempotencypersistence.New(database.SQL)),
				Jobs:        jobs.New(jobpersistence.New(database.SQL), now),
			},
			playDeps: PlayDependencies{
				Variants: variants,
				Launcher: launchcomposition.New(database.SQL,
					launch.NewSources(nil, nil), "", now, variants.Dispatch),
			},
			now: now,
			importDeps: ImportDependencies{
				Importer: importfixture.New(t, database.SQL, nil, importfixture.Options{Now: now}),
			},
		},
	}
	server.idempotencyQueueDrained = sync.NewCond(&server.idempotencyQueueMu)
	fixture := validationRetryFixture{server: server, now: now}
	fixture.jobID = seedValidationRetry(t, fixture)
	t.Cleanup(server.playDeps.Variants.Close)
	t.Cleanup(server.importDeps.Importer.Close)
	t.Cleanup(server.Wait)
	return fixture
}

func seedValidationRetry(t *testing.T, fixture validationRetryFixture) string {
	t.Helper()
	database := fixture.server.database
	now := fixture.now().UnixMilli()
	gameID, variantID := "01980000-0000-7000-8000-000000000191", "01980000-0000-7000-8000-000000000192"
	// The worker only needs relational content evidence; no runtime Provider build runs here.
	validationRetrySQL(t, database, `INSERT INTO games(id,platform_instance_id,title,title_initial,description,developer,publisher,genre,
metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,source_manifest_json,
source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms)
 VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='gbc/gambatte'),'Worker fixture',
'W','','','','','ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','fixture','{}',?,'PUBLISHED','worker fixture',
1,?,?)`, gameID, strings.Repeat("1", 64), now, now)
	validationRetrySQL(t, database, `INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order) VALUES(?,'CONTENT','worker.gbc',?,0)`, gameID, testsupport.FileMetadata("worker").Record)
	validationRetrySQL(t, database, `INSERT INTO game_variants(id,game_id,core_id,provider_id,target_id,status,compatibility_code,
dependency_snapshot_json,version,created_at_ms,updated_at_ms)
 SELECT ?,?,'gambatte',provider_id,target_id,'BLOCKED','VALIDATION_PENDING','{}',1,?,? FROM
runtime_target_bindings WHERE core_id='gambatte' LIMIT 1`, variantID, gameID, now, now)
	provisional := gamevariant.ValidationInputs{GameID: gameID, GameVariantID: variantID}
	facts, err := variantrepository.NewValidationWorker(database).Facts(t.Context(), provisional)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := gamevariant.Inputs(facts.Content, variantID)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	queued,
		err := gamevariant.NewValidationScheduler(variantrepository.NewValidationJobs(transaction),
		gamevariant.ValidationEnvironment{Now: fixture.now}).Queue(t.Context(), inputs)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	validationRetrySQL(t, database, `UPDATE jobs SET state='FAILED',error_code='LAUNCH_CORE_VALIDATION_UNAVAILABLE',error_retryable=1,
finished_at_ms=? WHERE id=?`, now, queued.JobID)
	return queued.JobID
}

func validationRetrySQL(t *testing.T, database dbapi.DB, statement string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}

func (fixture validationRetryFixture) request(ctx context.Context, writer http.ResponseWriter) {
	ctx = context.WithValue(ctx, operationIDContextKey, "postAdminJobRetry")
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/admin/jobs/"+fixture.jobID+"/retry", strings.NewReader(`{}`))
	request.SetPathValue("jobId", fixture.jobID)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", validationRetryKey)
	request.Header.Set("If-Match", `"v1"`)
	fixture.server.idempotencyHandler(http.HandlerFunc(fixture.server.retryJob)).ServeHTTP(writer, request)
}

func waitValidationRetry(t *testing.T, fixture validationRetryFixture) (string, int64, int64) {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		var execution, attempt int64
		if err := dbapi.QueryRowContext(t.Context(), fixture.server.database, `SELECT state,execution_no,attempt_count FROM jobs WHERE id=?`, fixture.jobID).Scan(&state, &execution, &attempt); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" || state == "FAILED" {
			return state, execution, attempt
		}
		select {
		case <-timeout.C:
			t.Fatalf("retry never resumed: %s/%d/%d", state, execution, attempt)
			return "", 0, 0
		case <-ticker.C:
		}
	}
}

func TestValidationRetryDispatchesOnlyAfterReceiptAndReplayKeepsExecution(t *testing.T) {
	fixture := newValidationRetryFixture(t)
	response := httptest.NewRecorder()
	fixture.request(t.Context(), response)
	if response.Code != http.StatusAccepted {
		t.Fatalf("retry %d: %s", response.Code, response.Body.String())
	}
	state, execution, attempt := waitValidationRetry(t, fixture)
	if state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatalf("retried worker %s/%d/%d", state, execution, attempt)
	}
	before := response.Body.String()
	replay := httptest.NewRecorder()
	fixture.request(t.Context(), replay)
	if replay.Code != http.StatusAccepted || replay.Body.String() != before {
		t.Fatalf("replay changed response: %d %s", replay.Code, replay.Body.String())
	}
	state, execution, attempt = waitValidationRetry(t, fixture)
	if state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatal("replay dispatched new execution")
	}
}

func TestValidationRetryReceiptFailureDoesNotDispatch(t *testing.T) {
	fixture := newValidationRetryFixture(t)
	hits := 0
	cause := errors.New("receipt write failed")
	source := fixture.server.database
	fault := testsupport.OpenSQLFaultDatabase(t, source,
		testsupport.SQLFaultHooks{BeforeExec: func(_ context.Context, query string,
			args []driver.NamedValue,
		) error {
			if strings.Contains(query, "INSERT INTO idempotency_records") && len(args) > 1 &&
				args[1].Value == "postAdminJobRetry" {
				hits++
				return cause
			}
			return nil
		}})
	fixture.server.systemDeps.Idempotency = idempotencyservice.New(idempotencypersistence.New(fault))
	response := httptest.NewRecorder()
	fixture.request(t.Context(), response)
	if response.Code != http.StatusInternalServerError || hits != 1 {
		t.Fatalf("receipt failure: %d %s", response.Code, response.Body.String())
	}
	var state string
	var attempt int64
	if err := dbapi.QueryRowContext(t.Context(), fixture.server.database, `SELECT state,attempt_count FROM jobs WHERE id=?`, fixture.jobID).Scan(&state, &attempt); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || attempt != 0 {
		t.Fatalf("receipt failure dispatched: %s/%d", state, attempt)
	}
}

type validationCancellingWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (writer validationCancellingWriter) WriteHeader(code int) {
	writer.cancel()
	writer.ResponseRecorder.WriteHeader(code)
}

func TestValidationRetryBackgroundStartSurvivesResponseCancellation(t *testing.T) {
	fixture := newValidationRetryFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := validationCancellingWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	fixture.request(ctx, writer)
	if writer.Code != http.StatusAccepted {
		t.Fatal(fmt.Sprintf("cancel retry: %d %s", writer.Code, writer.Body.String()))
	}
	state, execution, attempt := waitValidationRetry(t, fixture)
	if ctx.Err() == nil || state != "SUCCEEDED" || execution != 2 || attempt != 1 {
		t.Fatalf("background worker followed response cancellation: %s/%d/%d", state, execution, attempt)
	}
}
