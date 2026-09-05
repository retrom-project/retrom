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
	"retrom/internal/emulationstationimport"
	"retrom/internal/testassert"
)

func TestEmulationStationImportHTTPScanMappingSourceDriftAndDelete(t *testing.T) {
	server := newTestServer(t)
	server.emulationStationImports.Close()
	root := t.TempDir()
	library := filepath.Join(root, "library")
	if err := os.MkdirAll(library, 0o700); err != nil {
		t.Fatal(err)
	}
	gamelistPath := filepath.Join(library, "gamelist.xml")
	writeEmulationStationHTTPFixture(t, gamelistPath, "Fixture")
	if err := os.WriteFile(filepath.Join(library, "fixture.nes"), []byte("fixture-rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	server.emulationStationImports = emulationstationimport.New(
		server.database,
		server.blobs,
		server.importer,
		server.credentials,
		[]config.ServerImportRoot{{
			ID: "games", Label: "Game Library", Path: root, CanonicalPath: root,
		}},
		time.Now,
	)
	server.emulationStationImports.Start()
	seedEmulationStationHTTPArtifact(t, server)
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()

	createRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/admin/emulationstation-imports",
		strings.NewReader(`{"rootId":"games","sourceRelativePath":"library"}`),
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(createRequest, cookie, csrf)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return created.Code != http.StatusAccepted },
			func() bool { return strings.Contains(created.Body.String(), root) },
		),
		"create = %d %s",
		created.Code,
		created.Body.String(),
	)
	var summary emulationstationimport.Summary
	if err := json.Unmarshal(created.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	waitForEmulationStationMapping(t, handler, cookie, &summary)

	listResponse := emulationStationGET(
		t, handler, cookie,
		"/api/v1/admin/emulationstation-imports?state=AWAITING_MAPPING&limit=20",
	)
	var history struct {
		Items []emulationstationimport.Summary `json:"items"`
	}
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return listResponse.Code != http.StatusOK },
			func() bool { return json.Unmarshal(listResponse.Body.Bytes(), &history) != nil },
			func() bool { return len(history.Items) != 1 || history.Items[0].ID != summary.ID },
		),
		"list = %d %s",
		listResponse.Code,
		listResponse.Body.String(),
	)

	gamelists := emulationStationGET(
		t, handler, cookie,
		"/api/v1/admin/emulationstation-imports/"+summary.ID+"/gamelists?parseState=VALID",
	)
	var gamelistPage struct {
		Items []emulationstationimport.Gamelist `json:"items"`
	}
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return gamelists.Code != http.StatusOK },
			func() bool { return json.Unmarshal(gamelists.Body.Bytes(), &gamelistPage) != nil },
			func() bool {
				return len(gamelistPage.Items) != 1 || gamelistPage.Items[0].RelativePath != "gamelist.xml"
			},
		),
		"gamelists = %d %s",
		gamelists.Code,
		gamelists.Body.String(),
	)

	collections := emulationStationGET(
		t, handler, cookie,
		"/api/v1/admin/emulationstation-imports/"+summary.ID+"/collections",
	)
	var collectionPage struct {
		Items []emulationstationimport.Collection `json:"items"`
	}
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return collections.Code != http.StatusOK },
			func() bool { return json.Unmarshal(collections.Body.Bytes(), &collectionPage) != nil },
			func() bool { return len(collectionPage.Items) != 1 },
		),
		"collections = %d %s",
		collections.Code,
		collections.Body.String(),
	)

	items := emulationStationGET(
		t, handler, cookie,
		"/api/v1/admin/emulationstation-imports/"+summary.ID+"/items?outcome=PENDING",
	)
	var itemPage struct {
		Items []emulationstationimport.Item `json:"items"`
	}
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return items.Code != http.StatusOK },
			func() bool { return json.Unmarshal(items.Body.Bytes(), &itemPage) != nil },
			func() bool { return len(itemPage.Items) != 1 },
		),
		"items = %d %s",
		items.Code,
		items.Body.String(),
	)

	mapEmulationStationHTTPCollection(t, handler, cookie, csrf, &summary, collectionPage.Items[0].ID, server)
	writeEmulationStationHTTPFixture(t, gamelistPath, "Changed")
	assertEmulationStationHTTPSourceDrift(t, handler, cookie, csrf, summary, root)
	deleteEmulationStationHTTPPlan(t, handler, cookie, csrf, summary)
}

func TestEmulationStationImportHTTPQueryAndPreconditionBoundaries(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	identifier := uuid.NewString()

	invalidQuery := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/emulationstation-imports/"+identifier+"/gamelists?parseState=BROKEN",
	)
	testassert.Falsef(
		t,
		invalidQuery.Code != http.StatusBadRequest,
		"invalid parse state = %d %s",
		invalidQuery.Code,
		invalidQuery.Body.String(),
	)

	unknownQuery := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/emulationstation-imports/"+identifier+"/items?metadata=private",
	)
	testassert.Falsef(
		t,
		unknownQuery.Code != http.StatusBadRequest,
		"unknown item query = %d %s",
		unknownQuery.Code,
		unknownQuery.Body.String(),
	)

	for _, suffix := range []string{"/gamelists", "/collections", "/items"} {
		missing := emulationStationGET(
			t, handler, cookie, "/api/v1/admin/emulationstation-imports/"+identifier+suffix,
		)
		testassert.Falsef(
			t,
			missing.Code != http.StatusNotFound,
			"missing child list %s = %d %s",
			suffix,
			missing.Code,
			missing.Body.String(),
		)
	}

	for _, requestSpec := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/v1/admin/emulationstation-imports/" + identifier + "/cancel", `{"reason":"stop"}`},
		{http.MethodPost, "/api/v1/admin/emulationstation-imports/" + identifier + "/retry", `{}`},
		{http.MethodDelete, "/api/v1/admin/emulationstation-imports/" + identifier, ""},
	} {
		request := httptest.NewRequestWithContext(
			context.Background(), requestSpec.method, requestSpec.path, strings.NewReader(requestSpec.body),
		)
		if requestSpec.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		request.Header.Set("Idempotency-Key", uuid.NewString())
		setCSRFCredentials(request, cookie, csrf)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		testassert.Falsef(
			t,
			testassert.Any(
				func() bool { return response.Code != http.StatusPreconditionRequired },
				func() bool { return !strings.Contains(response.Body.String(), "PRECONDITION_REQUIRED") },
			),
			"missing If-Match for %s = %d %s",
			requestSpec.path,
			response.Code,
			response.Body.String(),
		)
	}
}

func writeEmulationStationHTTPFixture(t *testing.T, path, title string) {
	t.Helper()
	contents := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<gameList><game><path>./fixture.nes</path><name>` + title + `</name></game></gameList>`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func seedEmulationStationHTTPArtifact(t *testing.T, server *Server) {
	t.Helper()
	requireHTTPTestRuntimeTarget(t, server.database, "fceumm")
}

func waitForEmulationStationMapping(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	summary *emulationstationimport.Summary,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response := emulationStationGET(
			t, handler, cookie, "/api/v1/admin/emulationstation-imports/"+summary.ID,
		)
		if response.Code == http.StatusOK &&
			json.Unmarshal(response.Body.Bytes(), summary) == nil &&
			summary.State == "AWAITING_MAPPING" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return summary.State != "AWAITING_MAPPING" },
			func() bool { return summary.Counts.Gamelists != 1 },
			func() bool { return summary.Counts.Collections != 1 },
			func() bool { return summary.Counts.Games != 1 },
		),
		"scanned summary = %#v",
		*summary,
	)
}

func emulationStationGET(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	path string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func mapEmulationStationHTTPCollection(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	summary *emulationstationimport.Summary,
	collectionID string,
	server *Server,
) {
	t.Helper()
	var platformInstanceID string
	if err := server.database.QueryRowContext(
		context.Background(),
		`SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY sort_order,id LIMIT 1`,
	).Scan(&platformInstanceID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"mappings": []map[string]any{{
			"collectionId": collectionID, "action": "IMPORT",
			"platformInstanceId": platformInstanceID, "tagIds": []string{},
		}},
	})
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPut,
		"/api/v1/admin/emulationstation-imports/"+summary.ID+"/collection-mappings",
		strings.NewReader(string(body)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v`+jsonInt(summary.Version)+`"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return response.Code != http.StatusOK },
			func() bool { return json.Unmarshal(response.Body.Bytes(), summary) != nil },
			func() bool { return summary.Counts.MappedCollections != 1 },
		),
		"mapping = %d %s",
		response.Code,
		response.Body.String(),
	)
}

func assertEmulationStationHTTPSourceDrift(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	summary emulationstationimport.Summary,
	root string,
) {
	t.Helper()
	body := `{"version":` + jsonInt(summary.Version) + `}`
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/admin/emulationstation-imports/"+summary.ID+"/start",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v`+jsonInt(summary.Version)+`"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	testassert.Falsef(
		t,
		testassert.Any(
			func() bool { return response.Code != http.StatusConflict },
			func() bool {
				return !strings.Contains(response.Body.String(), "EMULATIONSTATION_SOURCE_CHANGED")
			},
			func() bool { return strings.Contains(response.Body.String(), root) },
		),
		"source drift = %d %s",
		response.Code,
		response.Body.String(),
	)
}

func deleteEmulationStationHTTPPlan(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	summary emulationstationimport.Summary,
) {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		"/api/v1/admin/emulationstation-imports/"+summary.ID,
		nil,
	)
	request.Header.Set("If-Match", `"v`+jsonInt(summary.Version)+`"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	testassert.Falsef(t, response.Code != http.StatusNoContent, "delete = %d %s", response.Code, response.Body.String())

	missing := emulationStationGET(
		t, handler, cookie, "/api/v1/admin/emulationstation-imports/"+summary.ID,
	)
	testassert.Falsef(t, missing.Code != http.StatusNotFound, "deleted detail = %d %s", missing.Code, missing.Body.String())
}
