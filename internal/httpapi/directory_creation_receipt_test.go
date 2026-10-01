package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/config"
	dbapi "retrom/internal/database"
	idempotencyservice "retrom/internal/service/idempotency"

	"github.com/google/uuid"
)

func TestDirectoryCreationDoesNotReadRetiredHTTPReceipts(t *testing.T) {
	server := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	auth := accountHTTPLogin(t, handler)
	body := `{"platformId":"gbc","defaultCoreId":"gambatte","name":"LegacyReceipt","description":""}`
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("If-Match", `"v8"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("X-Retrom-Csrf", auth.csrf)
	request.AddCookie(auth.cookie)
	var principalID string
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT id FROM users WHERE username='test'`).Scan(&principalID); err != nil {
		t.Fatal(err)
	}
	digest, ok := semanticRequestDigest(request, principalID, "postAdminPlatformInstance", []byte(body))
	if !ok {
		t.Fatal("cannot calculate prior HTTP digest")
	}
	original := []byte(`{"id":"01980000-0000-7000-8000-000000000099","legacy":true}`)
	now := server.now().UnixMilli()
	err := server.systemDeps.Idempotency.Store(t.Context(), "postAdminPlatformInstance", request.Header.Get("Idempotency-Key"), principalID,
		idempotencyservice.Receipt{RequestDigest: digest, HTTPStatus: http.StatusCreated, HeadersJSON: `{"Content-Type":"application/json; charset=utf-8","ETag":"\"v1\""}`, Body: original}, now, now+86400000)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Body.String() == string(original) || response.Header().Get("X-Retrom-Idempotent-Replay") != "" {
		t.Fatalf("retired receipt affected creation=%d %s", response.Code, response.Body.String())
	}
	var directories int
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT count(*) FROM platform_instances WHERE name='LegacyReceipt'`).Scan(&directories); err != nil {
		t.Fatal(err)
	}
	if directories != 1 {
		t.Fatalf("created directories=%d", directories)
	}
}
