package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbapi "retrom/internal/database"

	"github.com/google/uuid"
)

func TestPlatformDirectoryCreationOrderIsStableAndComplete(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	for index := 104; index >= 0; index-- {
		id := fmt.Sprintf("01980000-0000-7000-8000-%012d", index)
		created := int64(index/2 + 1)
		_, err := server.database.ExecContext(t.Context(), `INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,enabled,version,created_at_ms,updated_at_ms)
 VALUES(?,'gba','mgba',?,?,1,1,?,?)`, id, fmt.Sprintf("Directory %d", 104-index), id, created, created)
		if err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/api/v1/admin/platform-instances?platformId=gba", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list = %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Items []struct {
			ID          string `json:"id"`
			CreatedAtMS int64  `json:"createdAtMs"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) < 105 {
		t.Fatalf("directory list truncated: %d", len(response.Items))
	}
	var matched []string
	for index, item := range response.Items {
		if strings.HasPrefix(item.ID, "01980000-0000-7000-8000-") {
			matched = append(matched, item.ID)
		}
		if index == 0 {
			continue
		}
		previous := response.Items[index-1]
		if previous.CreatedAtMS > item.CreatedAtMS || (previous.CreatedAtMS == item.CreatedAtMS && previous.ID > item.ID) {
			t.Fatalf("creation order changed: %+v before %+v", previous, item)
		}
	}
	if len(matched) != 105 {
		t.Fatalf("missing directories: %d", len(matched))
	}
	if strings.Contains(recorder.Body.String(), `"sortOrder"`) {
		t.Fatal("directory response exposes removed order")
	}
	assertDirectorySchemaHasNoManualOrder(t, server)
}

func assertDirectorySchemaHasNoManualOrder(t *testing.T, server *testServer) {
	t.Helper()
	var columns int
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT count(*) FROM (SELECT column_name AS name,data_type AS type FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='platform_instances') WHERE name='sort_order'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Fatal("directory schema retains removed order")
	}
}

func TestPlatformDirectoryRejectsManualOrdering(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	cookie, csrfToken := testSessionCredentials()
	cases := []struct{ name, method, path, body string }{
		{"create", http.MethodPost, "/api/v1/admin/platform-instances", `{"platformId":"gba","defaultCoreId":"mgba","name":"Removed order","description":"","sortOrder":1}`},
		{"patch", http.MethodPatch, "/api/v1/admin/platform-instances/01980000-0000-7000-8000-000000000001", `{"sortOrder":1}`},
		{"reorder", http.MethodPut, "/api/v1/admin/platform-instances/order", `{"items":[]}`},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), item.method, item.path, strings.NewReader(item.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", uuid.NewString())
			request.Header.Set("If-Match", `"v1"`)
			setCSRFCredentials(request, cookie, csrfToken)
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			expected := http.StatusBadRequest
			if item.name == "reorder" {
				expected = http.StatusNotFound
			}
			if recorder.Code != expected {
				t.Fatalf("removed contract = %d %s, expected %d", recorder.Code, recorder.Body.String(), expected)
			}
		})
	}
}
