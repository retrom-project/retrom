package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"retrom/internal/authn"
	"retrom/internal/config"
)

func TestAdminUserDirectoryStorageFailureIsServerError(t *testing.T) {
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	if _, err := server.database.ExecContext(t.Context(), `DROP TABLE auth_sessions`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(authn.WithPrincipal(t.Context(), authn.Principal{UserID: "admin", Role: "ADMIN"}), http.MethodGet, "/api/v1/admin/users", nil)
	response := httptest.NewRecorder()
	server.adminUsers(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("directory storage failure returned %d", response.Code)
	}
}
