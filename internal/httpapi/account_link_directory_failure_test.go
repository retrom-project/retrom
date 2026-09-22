package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"retrom/internal/authn"
	"retrom/internal/config"
)

func TestAdminAccountLinkDirectoryStorageFailureIsServerError(t *testing.T) {
	server := newAuthHTTPServer(t, config.ModeTest)
	if _, err := server.database.ExecContext(t.Context(), `DROP TABLE account_links`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(authn.WithPrincipal(t.Context(), authn.Principal{UserID: "admin", Role: "ADMIN"}), http.MethodGet, "/api/v1/admin/invitations", nil)
	response := httptest.NewRecorder()
	server.adminAccountLinks(response, request, "INVITATION", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("link directory storage failure returned %d", response.Code)
	}
}
