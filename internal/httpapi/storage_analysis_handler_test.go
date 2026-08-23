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

	cleanupKey := uuid.NewString()
	cleanupRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/admin/storage-cleanups", nil,
	)
	cleanupRequest.Header.Set("Idempotency-Key", cleanupKey)
	cleanupRequest.Header.Set("X-Retrom-Csrf", "test-only")
	cleanupResponse := httptest.NewRecorder()
	handler.ServeHTTP(cleanupResponse, cleanupRequest)
	if cleanupResponse.Code != http.StatusAccepted {
		t.Fatalf("storage cleanup = %d %s", cleanupResponse.Code, cleanupResponse.Body.String())
	}
	var cleanupBody storageCleanupResponse
	if err := json.Unmarshal(cleanupResponse.Body.Bytes(), &cleanupBody); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return cleanupResponse.Header().Get("Cache-Control") != "private, no-store" },
		func() bool { return cleanupBody.ScheduledBlobCount != 1 },
		func() bool { return cleanupBody.ScheduledBytes != "42" },
		func() bool { return cleanupBody.AcceptedAtMS <= 0 },
	), "storage cleanup response = headers:%v body:%s", cleanupResponse.Header(), cleanupResponse.Body.String())

	replayRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/admin/storage-cleanups", nil,
	)
	replayRequest.Header.Set("Idempotency-Key", cleanupKey)
	replayRequest.Header.Set("X-Retrom-Csrf", "test-only")
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, replayRequest)
	testassert.Falsef(t, testassert.Any(
		func() bool { return replay.Code != http.StatusAccepted },
		func() bool { return replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" },
		func() bool { return replay.Body.String() != cleanupResponse.Body.String() },
	), "storage cleanup replay = %d %v %s", replay.Code, replay.Header(), replay.Body.String())
	var cleanupAuditCount int64
	if err := server.database.QueryRowContext(context.Background(), `
SELECT count(*) FROM audit_events WHERE action='STORAGE_CLEANUP_REQUESTED'
`).Scan(&cleanupAuditCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, cleanupAuditCount != 1, "storage cleanup audits = %d", cleanupAuditCount)
	missingKeyRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/admin/storage-cleanups", nil,
	)
	missingKeyRequest.Header.Set("X-Retrom-Csrf", "test-only")
	missingKey := httptest.NewRecorder()
	handler.ServeHTTP(missingKey, missingKeyRequest)
	testassert.Falsef(t, testassert.Any(
		func() bool { return missingKey.Code != http.StatusBadRequest },
		func() bool { return !strings.Contains(missingKey.Body.String(), "INVALID_IDEMPOTENCY_KEY") },
	), "storage cleanup missing key = %d %s", missingKey.Code, missingKey.Body.String())

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
	memberCleanupRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/admin/storage-cleanups", nil,
	)
	memberCleanupRequest.Header.Set("Idempotency-Key", uuid.NewString())
	memberCleanupRequest.Header.Set("X-Retrom-Csrf", "test-only")
	memberCleanup := httptest.NewRecorder()
	handler.ServeHTTP(memberCleanup, memberCleanupRequest)
	testassert.Falsef(t, testassert.Any(
		func() bool { return memberCleanup.Code != http.StatusForbidden },
		func() bool { return !strings.Contains(memberCleanup.Body.String(), "ADMIN_REQUIRED") },
	), "member cleanup = %d %s", memberCleanup.Code, memberCleanup.Body.String())

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
