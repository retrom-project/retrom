//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/service/accounts"
	"retrom/internal/service/scans"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

type replacementTemporary struct {
	mutex sync.Mutex
	value map[string]string
}

func (m *replacementTemporary) Get(_ context.Context, key string) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	value, exists := m.value[key]
	if !exists {
		return "", model.ErrContextExpired
	}
	return value, nil
}

func (m *replacementTemporary) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.value == nil {
		m.value = make(map[string]string)
	}
	m.value[key] = value
	return nil
}

func (m *replacementTemporary) Delete(_ context.Context, key string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.value, key)
	return nil
}

func TestContentReplacementHTTPReturnsOperationAndEnforcesAdministration(t *testing.T) {
	f := testsupport.Library(t)
	f.Publish(t)
	member := model.User{ID: uuid.NewString(), Username: "member", DisplayName: "Member", Role: "user", Status: "active"}
	if err := f.Repository.CreateUser(t.Context(), member, "test-hash", 1000); err != nil {
		t.Fatal(err)
	}
	for _, user := range []model.User{f.Principal.User, member} {
		if err := f.Repository.CreateSession(t.Context(), uuid.NewString(), user.ID, "replacement-"+user.ID, 1000); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		CookieName: "test_session", Origin: "http://retrom.test",
		Accounts: &accounts.Service{Repository: f.Repository, Now: func() time.Time { return time.UnixMilli(1000) }},
		Scans:    &scans.Service{Repository: f.Repository, Redis: &replacementTemporary{}, Now: time.Now, Context: t.Context()},
	}
	handler, err := s.Handler()
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/admin/games/" + f.Game.ID + "/content-replacement"
	send := func(method, path, body, userID string, validCSRF bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		r.Header.Set("Origin", s.Origin)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if userID != "" {
			token := "replacement-" + userID
			r.AddCookie(&http.Cookie{Name: s.CookieName, Value: token})
			if validCSRF {
				r.Header.Set("X-Retrom-Csrf", csrf(token))
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	assertReplacementReadAuthorization(t, send, base, f.Principal.User.ID, member.ID)
	body, err := json.Marshal(map[string]any{"path": filepath.Join(t.TempDir(), "missing.nes"), "version": 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := send(http.MethodPost, base, string(body), f.Principal.User.ID, false).Code; got != http.StatusForbidden {
		t.Fatalf("submission bypassed CSRF: %d", got)
	}
	accepted := send(http.MethodPost, base, string(body), f.Principal.User.ID, true)
	var operation model.ContentReplacement
	if accepted.Code != http.StatusAccepted || json.Unmarshal(accepted.Body.Bytes(), &operation) != nil || !model.UUID(operation.ID) {
		t.Fatalf("submission returned game detail instead of operation: %d %s", accepted.Code, accepted.Body)
	}
	s.Scans.Wait()
	assertReplacementHTTPProgress(t, send, base, operation.ID, f.Principal.User.ID)
	for _, item := range []struct {
		userID string
		csrf   bool
		status int
	}{{member.ID, true, http.StatusForbidden}, {f.Principal.User.ID, false, http.StatusForbidden}, {f.Principal.User.ID, true, http.StatusOK}} {
		w := send(http.MethodPost, base+"/"+operation.ID+"/cancel", "", item.userID, item.csrf)
		if w.Code != item.status {
			t.Fatalf("cancel authorization=%d want=%d", w.Code, item.status)
		}
	}
}

type replacementHTTPRequest func(method, path, body, userID string, validCSRF bool) *httptest.ResponseRecorder

func assertReplacementHTTPProgress(t *testing.T, send replacementHTTPRequest, base, operationID, userID string) {
	t.Helper()
	var operation model.ContentReplacement
	for _, path := range []string{base, base + "/" + operationID} {
		w := send(http.MethodGet, path, "", userID, false)
		if err := json.Unmarshal(w.Body.Bytes(), &operation); w.Code != http.StatusOK || err != nil || operation.Status != "failed" {
			t.Fatalf("progress=%+v status=%d err=%v", operation, w.Code, err)
		}
		if operation.Error == nil || operation.Error.Code != "CONTENT_REPLACEMENT_FAILED" || strings.Contains(operation.Error.Message, "missing.nes") {
			t.Fatal("progress lost safe failure details")
		}
	}
}

func assertReplacementReadAuthorization(t *testing.T, send replacementHTTPRequest, base, adminID, memberID string) {
	t.Helper()
	for _, item := range []struct {
		userID string
		status int
	}{{"", http.StatusUnauthorized}, {memberID, http.StatusForbidden}, {adminID, http.StatusNotFound}} {
		if got := send(http.MethodGet, base, "", item.userID, false).Code; got != item.status {
			t.Fatalf("read authorization=%d want=%d", got, item.status)
		}
	}
}
