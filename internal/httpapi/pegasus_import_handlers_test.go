package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/config"
	"retrom/internal/pegasusimport"
	"retrom/internal/testassert"
)

func TestPegasusImportHTTPScanMappingAndSourceDrift(t *testing.T) {
	server := newTestServer(t)
	server.pegasusImports.Close()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "library"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(root, "library", "metadata.pegasus.txt")
	if err := os.WriteFile(metadataPath, []byte("collection: Console\nshortname: nes\ngame: Fixture\nfile: fixture.nes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "library", "fixture.nes"), []byte("fixture-rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	server.pegasusImports = pegasusimport.New(
		server.database, server.blobs, server.importer, server.credentials,
		[]config.ServerImportRoot{{ID: "games", Label: "Game Library", Path: root, CanonicalPath: root}}, time.Now,
	)
	server.pegasusImports.Start()
	seedHTTPTestCoreArtifact(t, server.database, "pegasus-http-artifact", "fceumm", "data/pegasus-http.js", strings.Repeat("0", 64), "{}")
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()

	createRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/pegasus-imports", strings.NewReader(`{"rootId":"games","sourceRelativePath":"library"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(createRequest, cookie, csrf)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return created.Code != http.StatusAccepted }, func() bool { return strings.Contains(created.Body.String(), root) }), "create = %d %s", created.Code, created.Body.String())
	var summary pegasusimport.Summary
	if err := json.Unmarshal(created.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/pegasus-imports/"+summary.ID, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &summary) == nil && summary.State == "AWAITING_MAPPING" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return summary.State != "AWAITING_MAPPING" }, func() bool { return summary.Counts.Collections != 1 }, func() bool { return summary.Counts.Games != 1 }, func() bool { return summary.Counts.MappedCollections != 0 }), "scanned summary = %#v", summary)

	collectionsRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/pegasus-imports/"+summary.ID+"/collections", nil)
	collectionsRequest.AddCookie(cookie)
	collectionsResponse := httptest.NewRecorder()
	handler.ServeHTTP(collectionsResponse, collectionsRequest)
	var page struct {
		Items []pegasusimport.Collection `json:"items"`
	}
	testassert.Falsef(t, testassert.Any(func() bool { return collectionsResponse.Code != http.StatusOK }, func() bool { return json.Unmarshal(collectionsResponse.Body.Bytes(), &page) != nil }, func() bool { return len(page.Items) != 1 }, func() bool { return page.Items[0].MappingAction != nil }), "collections = %d %s", collectionsResponse.Code, collectionsResponse.Body.String())
	var platformInstanceID string
	if err := server.database.QueryRowContext(context.Background(), `SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY sort_order,id LIMIT 1`).Scan(&platformInstanceID); err != nil {
		t.Fatal(err)
	}
	mappingBody, _ := json.Marshal(map[string]any{"mappings": []map[string]any{{"collectionId": page.Items[0].ID, "action": "IMPORT", "platformInstanceId": platformInstanceID, "tagIds": []string{}}}})
	mappingRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/admin/pegasus-imports/"+summary.ID+"/collection-mappings", strings.NewReader(string(mappingBody)))
	mappingRequest.Header.Set("Content-Type", "application/json")
	mappingRequest.Header.Set("If-Match", `"v`+jsonInt(summary.Version)+`"`)
	mappingRequest.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(mappingRequest, cookie, csrf)
	mapped := httptest.NewRecorder()
	handler.ServeHTTP(mapped, mappingRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return mapped.Code != http.StatusOK }, func() bool { return json.Unmarshal(mapped.Body.Bytes(), &summary) != nil }, func() bool { return summary.Counts.MappedCollections != 1 }), "mapping = %d %s", mapped.Code, mapped.Body.String())

	if err := os.WriteFile(metadataPath, []byte("collection: Changed\ngame: Fixture\nfile: fixture.nes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	startBody := `{"version":` + jsonInt(summary.Version) + `}`
	startRequest := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/pegasus-imports/"+summary.ID+"/start", strings.NewReader(startBody))
	startRequest.Header.Set("Content-Type", "application/json")
	startRequest.Header.Set("If-Match", `"v`+jsonInt(summary.Version)+`"`)
	startRequest.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(startRequest, cookie, csrf)
	started := httptest.NewRecorder()
	handler.ServeHTTP(started, startRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return started.Code != http.StatusConflict }, func() bool { return !strings.Contains(started.Body.String(), "PEGASUS_SOURCE_CHANGED") }, func() bool { return strings.Contains(started.Body.String(), root) }), "source drift = %d %s", started.Code, started.Body.String())
}

func jsonInt(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
