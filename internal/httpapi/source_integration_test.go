//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/service/accounts"
	"retrom/internal/testsupport"
)

func TestAbsoluteSourceBrowsingRequiresAdministrator(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	member := model.User{ID: uuid.NewString(), Username: "member", DisplayName: "Member", Role: "user", Status: "active"}
	if err := f.Repository.CreateUser(t.Context(), member, "test-hash", 1000); err != nil {
		t.Fatal(err)
	}
	for _, user := range []model.User{f.Principal.User, member} {
		if err := f.Repository.CreateSession(t.Context(), uuid.NewString(), user.ID, "source-session-"+user.ID, 1000); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{CookieName: "test_session", Accounts: &accounts.Service{
		Repository: f.Repository, Now: func() time.Time { return time.UnixMilli(1000) },
	}}
	handler, err := s.Handler()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	child := filepath.Join(directory, "child")
	if err = os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		userID string
		status int
	}{{"", http.StatusUnauthorized}, {member.ID, http.StatusForbidden}, {f.Principal.User.ID, http.StatusOK}} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"/api/v1/admin/source-directories?path="+url.QueryEscape(directory), http.NoBody)
		if item.userID != "" {
			r.AddCookie(&http.Cookie{Name: s.CookieName, Value: "source-session-" + item.userID})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != item.status {
			t.Fatalf("status=%d want=%d", w.Code, item.status)
		}
		if item.status == http.StatusOK {
			var result model.List[model.SourceDirectory]
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Items) != 1 || result.Items[0].Path != child {
				t.Fatalf("directories=%+v error=%v", result, err)
			}
		}
	}
}
