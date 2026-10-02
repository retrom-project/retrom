package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"retrom/internal/config"
	dbapi "retrom/internal/database"
)

func TestProtectedListsReadWhileBackgroundWriterIsOccupied(t *testing.T) {
	server := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	login := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"test","password":"test"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", "http://localhost:3000")
	login.Header.Set("Sec-Fetch-Site", "same-origin")
	loggedIn := httptest.NewRecorder()
	handler.ServeHTTP(loggedIn, login)
	if loggedIn.Code != http.StatusOK {
		t.Fatalf("login status: %d", loggedIn.Code)
	}
	cookies := loggedIn.Result().Cookies()
	tx, err := server.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	if _, err := tx.ExecContext(t.Context(), "UPDATE users SET display_name=display_name"); err != nil {
		t.Fatal(err)
	}
	initialWaits := server.database.Stats().WaitCount
	for _, path := range []string{"/api/v1/home", "/api/v1/games?limit=50", "/api/v1/admin/reviews?limit=20", "/api/v1/admin/users?limit=20"} {
		t.Run(path, func(t *testing.T) {
			samples := make([]time.Duration, 5)
			for index := range samples {
				ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
				request := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
				request.AddCookie(cookies[0])
				response := httptest.NewRecorder()
				started := time.Now()
				handler.ServeHTTP(response, request)
				samples[index] = time.Since(started)
				requestErr := ctx.Err()
				cancel()
				if response.Code != http.StatusOK || requestErr != nil {
					t.Fatalf("protected list queued behind writer: status=%d error=%v", response.Code, requestErr)
				}
			}
			slices.Sort(samples)
			t.Logf("writer occupied: requests=5 p95=%s max=%s", samples[4], samples[4])
		})
	}
	if waits := server.database.Stats().WaitCount - initialWaits; waits != 0 {
		t.Fatalf("read requests waited for writer pool: %d", waits)
	}
}
