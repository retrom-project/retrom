package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	idempotencypersistence "retrom/internal/persistence/idempotency"
	idempotencyservice "retrom/internal/service/idempotency"
	"retrom/internal/store"
	"retrom/internal/testsupport/testpostgres"

	"github.com/google/uuid"
)

func TestSourceCommandsDoNotWaitForUnrelatedHandler(t *testing.T) {
	database, err := store.Open(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	server := &Server{now: time.Now, systemDeps: SystemDependencies{
		Idempotency: idempotencyservice.New(idempotencypersistence.New(database.SQL)),
	}}
	enteredSlow, enteredFast, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseSlow := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseSlow()
	handler := server.idempotencyHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/slow" {
			close(enteredSlow)
			<-release
		} else {
			close(enteredFast)
		}
		id := uuid.NewString()
		ctx := request.Context()
		err := dbapi.RetryTransaction(ctx, database.SQL, func(tx dbapi.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id,actor_kind,actor_label,action,resource_type,resource_id,created_at_ms) VALUES(?,'SYSTEM','startup-test-bootstrap','PROBE','FIXTURE',?,0)`, id, request.URL.Path); err != nil {
				return err
			}
			return idempotencyservice.Complete(ctx, idempotencyservice.Result{Value: map[string]string{"id": id}})
		})
		if err != nil {
			server.databaseError(writer, request, err)
			return
		}
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte("{}"))
	}))
	slowResponse, fastResponse := httptest.NewRecorder(), httptest.NewRecorder()
	slowDone, fastDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(slowDone)
		handler.ServeHTTP(slowResponse, sourceCommandRequest(t.Context(),
			"/slow", "postAdminSourceImport", "01980000-0000-7000-8000-000000000071"))
	}()
	select {
	case <-enteredSlow:
	case <-time.After(5 * time.Second):
		t.Fatal("slow request did not enter its handler")
	}
	go func() {
		defer close(fastDone)
		handler.ServeHTTP(fastResponse, sourceCommandRequest(t.Context(),
			"/fast", "putAdminSourceImportCollectionMappings", "01980000-0000-7000-8000-000000000072"))
	}()
	select {
	case <-enteredFast:
	case <-time.After(500 * time.Millisecond):
		t.Error("unrelated Source command waited for the slow handler")
	}
	releaseSlow()
	<-slowDone
	<-fastDone
	if slowResponse.Code != 202 || fastResponse.Code != 200 {
		t.Fatalf("command commits failed: %d/%s / %d/%s", slowResponse.Code, slowResponse.Body.String(), fastResponse.Code, fastResponse.Body.String())
	}
	var writes, receipts int
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT (SELECT count(*) FROM audit_events WHERE action='PROBE'),(SELECT count(*) FROM idempotency_records)`).Scan(&writes, &receipts); err != nil {
		t.Fatal(err)
	}
	if writes != 2 || receipts != 2 {
		t.Fatalf("commands did not commit writes and receipts: %d/%d", writes, receipts)
	}
}

func sourceCommandRequest(ctx context.Context, path, operation, key string) *http.Request {
	request := httptest.NewRequestWithContext(context.WithValue(ctx, operationIDContextKey, operation),
		http.MethodPost, path, strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	return request
}
