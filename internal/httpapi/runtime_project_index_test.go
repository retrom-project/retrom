package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/launch"
)

func TestGeneratedProjectIndexOnlyStaticFormatFallsBack(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cause   error
		handled bool
		status  int
		body    string
	}{
		{name: "static index", cause: launch.ErrProjectIndexUnavailable, handled: false, status: 200},
		{name: "credential", cause: launch.ErrCredential, handled: true, status: 401, body: "LAUNCH_CREDENTIAL_INVALID"},
		{name: "storage", cause: errors.New("index read failed"), handled: true, status: 500, body: "INTERNAL_ERROR"},
		{name: "cancelled", cause: context.Canceled, handled: true, status: 200},
		{name: "deadline", cause: context.DeadlineExceeded, handled: true, status: 200},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			server := &Server{}
			writer := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/runtime/content/project/identity/index.json", nil)
			handled := server.writeGeneratedProjectIndex(writer, request, launch.ProjectIndexView{}, item.cause)
			if handled != item.handled || writer.Code != item.status {
				t.Fatalf("handled=%t status=%d body=%s", handled, writer.Code, writer.Body.String())
			}
			if item.body == "" && writer.Body.Len() != 0 {
				t.Fatalf("unexpected body: %s", writer.Body.String())
			}
			if item.body != "" && !strings.Contains(writer.Body.String(), item.body) {
				t.Fatalf("missing error code: %s", writer.Body.String())
			}
		})
	}
}

func TestGeneratedProjectIndexPreservesJSONHeadersAndHEAD(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		server := &Server{}
		writer := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), method, "/runtime/content/project/identity/index.json", nil)
		index := launch.ProjectIndexView{Contents: []byte(`{"files":[],"schemaVersion":1}`), SHA256: strings.Repeat("a", 64)}
		if !server.writeGeneratedProjectIndex(writer, request, index, nil) {
			t.Fatal("generated index fell back")
		}
		if writer.Code != 200 || writer.Header().Get("ETag") != `"sha256-`+index.SHA256+`"` || writer.Header().Get("Cache-Control") != "private, max-age=31536000, immutable, no-transform" {
			t.Fatalf("response=%d headers=%v", writer.Code, writer.Header())
		}
		if method == http.MethodHead && writer.Body.Len() != 0 {
			t.Fatal("HEAD included body")
		}
		if method == http.MethodGet && writer.Body.String() != string(index.Contents) {
			t.Fatal("GET changed bytes")
		}
	}
}
