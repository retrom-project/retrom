package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"retrom/internal/config"
	dbapi "retrom/internal/database"

	"github.com/google/uuid"
)

type countedRequestBody struct {
	reader    *strings.Reader
	readBytes int
}

func (body *countedRequestBody) Read(buffer []byte) (int, error) {
	count, err := body.reader.Read(buffer)
	body.readBytes += count
	return count, err
}
func (body *countedRequestBody) Close() error { return nil }

func TestLoginBodyLimitRunsBeforeOpenAPI(t *testing.T) {
	server := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	for _, unknownLength := range []bool{false, true} {
		for _, size := range []int{32 << 10, (32 << 10) + 1, 128 << 10} {
			t.Run(fmt.Sprintf("unknown-length=%t/size=%d", unknownLength, size), func(t *testing.T) {
				assertLoginLimit(t, handler, unknownLength, size)
			})
		}
	}
}

func assertLoginLimit(t *testing.T, handler http.Handler, unknownLength bool, size int) {
	t.Helper()
	login := `{"username":"test","password":"test"}`
	payload := strings.Repeat(" ", size-len(login)) + login
	counter := &countedRequestBody{reader: strings.NewReader(payload)}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", nil)
	request.Body = counter
	request.ContentLength = int64(len(payload))
	if unknownLength {
		request.ContentLength = -1
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if counter.readBytes > (32<<10)+1 {
		t.Fatalf("pre-parser read=%d", counter.readBytes)
	}
	expected := http.StatusOK
	if size > 32<<10 {
		expected = http.StatusRequestEntityTooLarge
	}
	if response.Code != expected {
		t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
	}
	if size > 32<<10 && !unknownLength && counter.readBytes != 0 {
		t.Fatalf("known oversized body read=%d", counter.readBytes)
	}
	if expected == http.StatusRequestEntityTooLarge && !strings.Contains(response.Body.String(), "REQUEST_TOO_LARGE") {
		t.Fatal("missing public size error")
	}
}

func TestStreamingBodyLimitDoesNotReadAhead(t *testing.T) {
	server := newTestServer(t)
	payload := strings.Repeat("x", (10<<20)+2)
	counter := &countedRequestBody{reader: strings.NewReader(payload)}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"/runtime/launches/01980000-0000-7000-8000-000000000001/review-screenshot", nil)
	request.Header.Set("Content-Type", "image/png")
	request.Body = counter
	request.ContentLength = -1
	called := false
	response := httptest.NewRecorder()
	server.openAPIHandler(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		called = true
		if counter.readBytes != 0 {
			t.Fatalf("streaming parser read ahead=%d", counter.readBytes)
		}
		_, err := io.Copy(io.Discard, incoming.Body)
		if err == nil {
			t.Fatal("unbounded streaming request")
		}
		writeRequestTooLarge(writer, incoming)
	})).ServeHTTP(response, request)
	if !called || response.Code != http.StatusRequestEntityTooLarge || counter.readBytes > (10<<20)+2 {
		t.Fatalf("called=%t status=%d read=%d", called, response.Code, counter.readBytes)
	}
}

func directoryCreationProbe(t *testing.T) (*testServer, func(string, string) *httptest.ResponseRecorder) {
	t.Helper()
	server := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	auth := accountHTTPLogin(t, handler)
	create := func(key, name string) *httptest.ResponseRecorder {
		payload := fmt.Sprintf(`{"platformId":"gbc","defaultCoreId":"gambatte","name":%q,"description":"","sortOrder":900}`, name)
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		request.Header.Set("Origin", "http://localhost:3000")
		request.Header.Set("X-Retrom-Csrf", auth.csrf)
		request.AddCookie(auth.cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	return server, create
}

func directoryCreationCounts(t *testing.T, server *testServer, key string) (int, int) {
	t.Helper()
	var directories, receipts int
	err := dbapi.QueryRowContext(t.Context(), server.database,
		`SELECT (SELECT count(*) FROM platform_instances WHERE name='ReviewReceiptProbe'),
  (SELECT count(*) FROM idempotency_records WHERE operation_id='postAdminPlatformInstance' AND key=?)`, key,
	).Scan(&directories, &receipts)
	if err != nil {
		t.Fatal(err)
	}
	return directories, receipts
}

func TestDirectoryCreationRollsBackWhenReceiptFails(t *testing.T) {
	server, create := directoryCreationProbe(t)
	_, err := server.database.ExecContext(t.Context(),
		`ALTER TABLE idempotency_records ADD COLUMN receipt_guard INTEGER CHECK(operation_id!='postAdminPlatformInstance')`)
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	first := create(key, "ReviewReceiptProbe")
	directories, receipts := directoryCreationCounts(t, server, key)
	if first.Code != http.StatusInternalServerError || directories != 0 || receipts != 0 {
		t.Fatalf("receipt failure: HTTP=%d directories=%d receipts=%d", first.Code, directories, receipts)
	}
	var audits int
	if err := dbapi.QueryRowContext(t.Context(), server.database,
		`SELECT count(*) FROM audit_events WHERE action='PLATFORM_INSTANCE_CREATED'`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("creation audit survived rollback: %d", audits)
	}
	_, err = server.database.ExecContext(t.Context(), `ALTER TABLE idempotency_records DROP COLUMN receipt_guard`)
	if err != nil {
		t.Fatal(err)
	}
	retry := create(key, "ReviewReceiptProbe")
	directories, receipts = directoryCreationCounts(t, server, key)
	if retry.Code != http.StatusCreated || directories != 1 || receipts != 1 {
		t.Fatalf("retry: HTTP=%d directories=%d receipts=%d body=%s", retry.Code, directories, receipts, retry.Body.String())
	}
	replay := create(key, "ReviewReceiptProbe")
	assertDirectoryReplay(t, retry, replay)
}

func assertDirectoryReplay(t *testing.T, original, replay *httptest.ResponseRecorder) {
	t.Helper()
	if replay.Code != http.StatusCreated || replay.Body.String() != original.Body.String() ||
		replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" || replay.Header().Get("ETag") != `"v1"` ||
		replay.Header().Get("Content-Type") != original.Header().Get("Content-Type") {
		t.Fatalf("replay=%d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
}

func TestConcurrentDirectoryCreationConvergesAndRejectsDifferentInput(t *testing.T) {
	server, create := directoryCreationProbe(t)
	key := uuid.NewString()
	var workers sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, 8)
	for index := range responses {
		workers.Go(func() { responses[index] = create(key, "ReviewReceiptProbe") })
	}
	workers.Wait()
	var original *httptest.ResponseRecorder
	for _, response := range responses {
		if response.Code != http.StatusCreated {
			t.Fatalf("creation=%d %s", response.Code, response.Body.String())
		}
		if response.Header().Get("X-Retrom-Idempotent-Replay") == "" {
			if original != nil {
				t.Fatal("multiple fresh creations")
			}
			original = response
		}
	}
	if original == nil {
		t.Fatal("no initial creation")
	}
	for _, response := range responses {
		if response != original {
			assertDirectoryReplay(t, original, response)
		}
	}
	directories, receipts := directoryCreationCounts(t, server, key)
	if directories != 1 || receipts != 1 {
		t.Fatalf("directories=%d receipts=%d", directories, receipts)
	}
	conflict := create(key, "DifferentDirectory")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("reuse=%d %s", conflict.Code, conflict.Body.String())
	}
}
