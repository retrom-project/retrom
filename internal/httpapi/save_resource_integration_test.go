//go:build integration

package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/service/accounts"
	"retrom/internal/service/saves"
	"retrom/internal/storage"
	"retrom/internal/testsupport"
)

func saveResourceFixture(t *testing.T) (*Server, testsupport.Fixture, model.Save, []byte) {
	t.Helper()
	f := testsupport.Library(t)
	f.Publish(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	payload := []byte("actual immutable checkpoint bytes")
	id := uuid.NewString()
	file, err := store.Write(t.Context(), "saves", id, bytes.NewReader(payload), 1024)
	if err != nil {
		t.Fatal(err)
	}
	save := model.Save{
		ID: id, UserID: f.Principal.User.ID, Game: model.Game{ID: f.Game.ID},
		Kind: "checkpoint", Name: "Conditional restore", StorageKey: file.Key, PayloadHash: file.SHA256,
		SizeBytes: file.Size, LastCommitID: uuid.NewString(),
		Extinfo: model.Extinfo{RuntimeOptions: json.RawMessage(`{}`), Content: json.RawMessage(`{}`)},
	}
	if err = f.Repository.WriteSave(t.Context(), save, 1000, true); err != nil {
		t.Fatal(err)
	}
	if err = f.Repository.CreateSession(t.Context(), uuid.NewString(), f.Principal.User.ID,
		"session-"+f.Principal.User.ID, 1000); err != nil {
		t.Fatal(err)
	}
	return &Server{
		Saves: &saves.Service{Repository: f.Repository}, Storage: store, CookieName: "test_session",
		Accounts: &accounts.Service{Repository: f.Repository, Now: func() time.Time { return time.UnixMilli(1000) }},
	}, f, save, payload
}

func requestSaveFile(t *testing.T, s *Server, p model.Principal, id, method, match, span string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/saves/"+id+"/payload", http.NoBody)
	r.SetPathValue("saveId", id)
	r.Header.Set("If-Match", match)
	r.Header.Set("Range", span)
	r.AddCookie(&http.Cookie{Name: s.CookieName, Value: "session-" + p.User.ID})
	w := httptest.NewRecorder()
	handler, err := s.Handler()
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(w, r)
	return w
}

func TestSavePayloadConditionalColdHTTP(t *testing.T) {
	t.Parallel()
	s, f, save, payload := saveResourceFixture(t)
	tag := `"sha256-` + save.PayloadHash + `"`
	cases := []struct {
		name, method, match, span string
		status                    int
		body                      []byte
		length                    int
	}{
		{"get", http.MethodGet, "", "", http.StatusOK, payload, len(payload)},
		{"matched", http.MethodGet, tag, "", http.StatusOK, payload, len(payload)},
		{"head", http.MethodHead, tag, "", http.StatusOK, nil, len(payload)},
		{"range", http.MethodGet, tag, "bytes=3-7", http.StatusPartialContent, payload[3:8], 5},
		{"head range", http.MethodHead, tag, "bytes=3-7", http.StatusPartialContent, nil, 5},
		{"changed", http.MethodGet, `"sha256-other"`, "", http.StatusPreconditionFailed, nil, 0},
		{"changed head", http.MethodHead, `"sha256-other"`, "", http.StatusPreconditionFailed, nil, 0},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			w := requestSaveFile(t, s, f.Principal, save.ID, item.method, item.match, item.span)
			if w.Code != item.status || !bytes.Equal(w.Body.Bytes(), item.body) || w.Header().Get("ETag") != tag {
				t.Fatalf("status=%d tag=%q body=%q", w.Code, w.Header().Get("ETag"), w.Body.Bytes())
			}
			if item.status != http.StatusPreconditionFailed && w.Header().Get("Content-Length") != strconv.Itoa(item.length) {
				t.Fatalf("length=%q want=%d", w.Header().Get("Content-Length"), item.length)
			}
			if item.status == http.StatusPartialContent && w.Header().Get("Content-Range") != "bytes 3-7/"+strconv.Itoa(len(payload)) {
				t.Fatalf("content range=%q", w.Header().Get("Content-Range"))
			}
		})
	}
}

func TestOverwrittenSaveDoesNotMislabelOldPayload(t *testing.T) {
	t.Parallel()
	s, f, save, original := saveResourceFixture(t)
	projection, err := s.Saves.File(t.Context(), f.Principal, save.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("new immutable checkpoint after overwrite")
	prepared, err := s.Storage.Write(t.Context(), "saves", save.ID, bytes.NewReader(replacement), 1024)
	if err != nil {
		t.Fatal(err)
	}
	save.Version, save.StorageKey, save.PayloadHash = 1, prepared.Key, prepared.SHA256
	save.SizeBytes, save.LastCommitID = prepared.Size, uuid.NewString()
	if err = f.Repository.WriteSave(t.Context(), save, 2000, false); err != nil {
		t.Fatal(err)
	}
	// A reader that already selected the immutable file keeps its matching old hash.
	oldFile, err := s.Storage.Read(projection.Key)
	if err != nil {
		t.Fatal(err)
	}
	old, readErr := io.ReadAll(oldFile)
	closeErr := oldFile.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(old, original) || projection.SHA256 == prepared.SHA256 {
		t.Fatalf("frozen payload=%q read=%v close=%v", old, readErr, closeErr)
	}
	oldTag, currentTag := `"sha256-`+projection.SHA256+`"`, `"sha256-`+prepared.SHA256+`"`
	changed := requestSaveFile(t, s, f.Principal, save.ID, http.MethodGet, oldTag, "")
	if changed.Code != http.StatusPreconditionFailed || changed.Body.Len() != 0 || changed.Header().Get("ETag") != currentTag {
		t.Fatalf("old identity received replacement: status=%d body=%q", changed.Code, changed.Body.Bytes())
	}
	current := requestSaveFile(t, s, f.Principal, save.ID, http.MethodGet, currentTag, "bytes=0-3")
	if current.Code != http.StatusPartialContent || !bytes.Equal(current.Body.Bytes(), replacement[:4]) ||
		current.Header().Get("ETag") != currentTag {
		t.Fatalf("new identity failed: status=%d tag=%q", current.Code, current.Header().Get("ETag"))
	}
}
