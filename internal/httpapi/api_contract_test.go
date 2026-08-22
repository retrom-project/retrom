package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"retrom/internal/httpapi/generated"
	"retrom/internal/testassert"
)

func TestOpenAPIValidationAllowsNestedRuntimePath(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	server.Handler().
		ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodHead, "/runtime/emulatorjs/4.2.3/data/loader.js", nil))
	testassert.Falsef(t, recorder.Code != http.StatusOK, "nested runtime path status = %d, body=%s", recorder.Code, recorder.Body.String())
	cacheBusted := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		cacheBusted,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/runtime/emulatorjs/4.2.3/data/loader.js?v=496182", nil),
	)
	testassert.Falsef(t, cacheBusted.Code != http.StatusOK, "runtime cache-buster status = %d, body=%s", cacheBusted.Code, cacheBusted.Body.String())
}

func TestOpenAPIValidationAllowsPrereleaseRuntimeVersion(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(context.Background(), http.MethodHead, "/runtime/emulatorjs/4.3.0-pre/data/loader.js", nil),
	)
	testassert.Falsef(t, recorder.Code != http.StatusNotFound, "unconfigured prerelease runtime status = %d, body=%s", recorder.Code, recorder.Body.String())
}

func TestOpenAPIValidationRejectsUnknownJSONAndMapsMissingPrecondition(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	handler := server.Handler()
	cookie, csrfToken := testSessionCredentials()
	request := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost,
		"/api/v1/launches",
		strings.NewReader(
			`{"gameId":"01980000-0000-7000-8000-000000000001","coreId":null,"saveStateId":null,"dosEntry":null,"returnTo":"/","clientCapabilities":{"secureContext":true,"crossOriginIsolated":true,"sharedArrayBuffer":true},"unknown":true}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "01980000-0000-7000-8000-000000000099")
	setCSRFCredentials(request, cookie, csrfToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	testassert.Falsef(t, testassert.Any(func() bool { return recorder.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(recorder.Body.String(), `"code":"INVALID_REQUEST"`) }), "unknown JSON response = %d %s", recorder.Code, recorder.Body.String())

	request = httptest.NewRequestWithContext(context.Background(),
		http.MethodPatch,
		"/api/v1/saves/01980000-0000-7000-8000-000000000001",
		strings.NewReader(`{"name":"slot"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	setCSRFCredentials(request, cookie, csrfToken)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	testassert.Falsef(t, testassert.Any(func() bool { return recorder.Code != http.StatusPreconditionRequired }, func() bool { return !strings.Contains(recorder.Body.String(), `"code":"PRECONDITION_REQUIRED"`) }), "missing If-Match response = %d %s", recorder.Code, recorder.Body.String())
}

func TestOpenAPIHasExactlyFourStreamingOperations(t *testing.T) {
	t.Parallel()
	specification, err := generated.GetSpec()
	testassert.False(t, err != nil, err)
	operationIDs := make([]string, 0, 4)
	for _, pathItem := range specification.Paths.Map() {
		for _, operation := range pathItem.Operations() {
			if enabled, ok := operation.Extensions["x-retrom-streaming-body"].(bool); ok && enabled {
				operationIDs = append(operationIDs, operation.OperationID)
			}
		}
	}
	slices.Sort(operationIDs)
	wanted := []string{"PostRuntimeReviewScreenshot", "PostRuntimeSaveState", "PutAdminUploadPart", "PutRuntimePersistentSave"}
	testassert.Truef(t, slices.Equal(operationIDs, wanted), "streaming operations = %v", operationIDs)
}

func TestReviewScreenshotValidatesMediaTypeAndCredentialBeforeReadingBody(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)

	invalidType := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/runtime/launches/preview/review-screenshot", strings.NewReader("not png"))
	invalidType.SetPathValue("launchId", "01980000-0000-7000-8000-000000000099")
	invalidType.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	server.storeReviewScreenshot(response, invalidType)
	testassert.Falsef(t, testassert.Any(func() bool { return response.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(response.Body.String(), `"code":"REVIEW_SCREENSHOT_INVALID"`) }), "invalid review screenshot media type = %d %s", response.Code, response.Body.String())

	unauthorized := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost,
		"/runtime/launches/preview/review-screenshot",
		io.NopCloser(panicReader{}),
	)
	unauthorized.SetPathValue("launchId", "01980000-0000-7000-8000-000000000099")
	unauthorized.Header.Set("Content-Type", "image/png")
	response = httptest.NewRecorder()
	server.storeReviewScreenshot(response, unauthorized)
	testassert.Falsef(t, testassert.Any(func() bool { return response.Code != http.StatusUnauthorized }, func() bool { return !strings.Contains(response.Body.String(), `"code":"LAUNCH_CREDENTIAL_INVALID"`) }), "unauthorized review screenshot = %d %s", response.Code, response.Body.String())
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) {
	panic("review screenshot body was read before credential validation")
}

func TestGenericIdempotencySerializesConcurrentCreates(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	handler := server.Handler()
	cookie, csrfToken := testSessionCredentials()
	key := "01980000-0000-7000-8000-000000000077"
	body := `{"platformId":"nes","defaultCoreId":"fceumm","name":"Concurrent Directory","description":"","sortOrder":99}`
	send := func(contents string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(contents))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		setCSRFCredentials(request, cookie, csrfToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	responses := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for index := range responses {
		wait.Add(1)
		go func() {
			defer wait.Done()
			responses[index] = send(body)
		}()
	}
	wait.Wait()
	testassert.Falsef(t, testassert.Any(func() bool { return responses[0].Code != http.StatusCreated }, func() bool { return responses[1].Code != http.StatusCreated }, func() bool { return responses[0].Body.String() != responses[1].Body.String() }), "idempotent responses = %d/%d %q/%q", responses[0].Code, responses[1].Code, responses[0].Body.String(), responses[1].Body.String())
	var count int
	if err := server.database.QueryRowContext(context.Background(), `
SELECT count(*)
FROM platform_instances
WHERE slug='concurrent-directory'
`).Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("created rows = %d, error=%v", count, err)
	}
	conflict := send(
		`{"platformId":"nes","defaultCoreId":"fceumm","name":"Different Directory","description":"","sortOrder":99}`,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return conflict.Code != http.StatusConflict }, func() bool { return !strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) }), "idempotency conflict = %d %s", conflict.Code, conflict.Body.String())
}
