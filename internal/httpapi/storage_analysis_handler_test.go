package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/testassert"
)

func TestAdminStorageAnalysisContractAndAccess(t *testing.T) {
	server := newTestServer(t)
	if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES('storage-test','0000000000000000000000000000000000000000000000000000000000000001',42,
'00000000000000000000000000000001','0000000000000000000000000000000000000001','00000001',
'application/octet-stream',0)`); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/admin/storage-analysis", nil,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("storage analysis = %d %s", response.Code, response.Body.String())
	}
	var body storageAnalysisResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return response.Header().Get("Cache-Control") != "private, no-store" },
		func() bool { return body.Scope != "REGISTERED_CAS_PAYLOAD_V1" },
		func() bool { return body.Totals.RegisteredBytes != "42" },
		func() bool { return body.Totals.ProtectedBytes != "0" },
		func() bool { return body.Totals.UnreferencedBytes != "42" },
		func() bool { return body.Totals.BlobCount != 1 },
		func() bool { return len(body.Categories) != 9 },
		func() bool { return len(body.Excluded) != 7 },
		func() bool { return strings.Contains(response.Body.String(), "storage-test") },
		func() bool { return strings.Contains(response.Body.String(), "sha256") },
	), "storage response = headers:%v body:%s", response.Header(), response.Body.String())

	unknownQuery := httptest.NewRecorder()
	handler.ServeHTTP(unknownQuery, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/admin/storage-analysis?scope=all", nil,
	))
	testassert.Falsef(t, unknownQuery.Code != http.StatusBadRequest,
		"storage query = %d %s", unknownQuery.Code, unknownQuery.Body.String())

	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: uuid.NewString(), ProfileID: uuid.NewString(), Username: "member", DisplayName: "Member", Role: "USER",
	}}
	member := httptest.NewRecorder()
	handler.ServeHTTP(member, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/admin/storage-analysis", nil,
	))
	testassert.Falsef(t, testassert.Any(
		func() bool { return member.Code != http.StatusForbidden },
		func() bool { return !strings.Contains(member.Body.String(), "ADMIN_REQUIRED") },
	), "member storage = %d %s", member.Code, member.Body.String())

	server.authenticator = fixedAuthenticator{Err: accounts.ErrAuthenticationNeeded}
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/admin/storage-analysis", nil,
	))
	testassert.Falsef(t, testassert.Any(
		func() bool { return anonymous.Code != http.StatusUnauthorized },
		func() bool { return !strings.Contains(anonymous.Body.String(), "AUTHENTICATION_REQUIRED") },
	), "anonymous storage = %d %s", anonymous.Code, anonymous.Body.String())
}

func TestAdminStorageAnalysisReadFailure(t *testing.T) {
	server := newTestServer(t)
	server.storageAnalysis = nil
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/admin/storage-analysis", nil,
	))
	testassert.Falsef(t, testassert.Any(
		func() bool { return response.Code != http.StatusInternalServerError },
		func() bool { return !strings.Contains(response.Body.String(), "INTERNAL_ERROR") },
	), "missing storage database = %d %s", response.Code, response.Body.String())
}
