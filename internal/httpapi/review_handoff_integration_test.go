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
	"retrom/internal/libraryimport"
	"retrom/internal/testassert"
)

func TestEmulationStationReviewHandoffReservationBlocksEveryReviewEntry(t *testing.T) {
	server, handler, cookie, csrf, summary := startReservedReviewHandoffFixture(t)
	itemID := waitForBlockedReviewHandoff(t, server, summary.ID)
	assertReviewHandoffBlocked(t, server, handler, cookie, csrf, summary.ID, itemID)

	if _, err := server.database.ExecContext(
		context.Background(),
		"DROP TRIGGER test_fail_before_emulationstation_review_attach",
	); err != nil {
		t.Fatal(err)
	}
	retryEmulationStationReviewHandoff(t, handler, cookie, csrf, &summary)
	waitForCompletedReviewHandoff(t, server, handler, cookie, &summary, itemID)
	assertReviewHandoffAvailable(t, server, handler, cookie, csrf, summary.ID, itemID)
}

func startReservedReviewHandoffFixture(
	t *testing.T,
) (*Server, http.Handler, *http.Cookie, string, emulationstationimport.Summary) {
	t.Helper()
	server := newTestServer(t)
	server.emulationStationImports.Close()
	root := t.TempDir()
	library := filepath.Join(root, "library")
	if err := os.MkdirAll(library, 0o700); err != nil {
		t.Fatal(err)
	}
	writeEmulationStationHTTPFixture(t, filepath.Join(library, "gamelist.xml"), "Reserved fixture")
	if err := os.WriteFile(filepath.Join(library, "fixture.nes"), []byte("reserved-rom"), 0o600); err != nil {
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
	if _, err := server.database.ExecContext(context.Background(), `
CREATE TRIGGER test_fail_before_emulationstation_review_attach
BEFORE UPDATE OF library_import_item_id ON emulationstation_import_items
WHEN OLD.library_import_item_id IS NULL AND NEW.library_import_item_id IS NOT NULL
BEGIN SELECT RAISE(ABORT,'test failpoint before EmulationStation review attach'); END;
`); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	summary := createEmulationStationReviewPlan(t, server, handler, cookie, csrf)
	return server, handler, cookie, csrf, summary
}

func createEmulationStationReviewPlan(
	t *testing.T,
	server *Server,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
) emulationstationimport.Summary {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/admin/emulationstation-imports",
		strings.NewReader(`{"rootId":"games","sourceRelativePath":"library"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var summary emulationstationimport.Summary
	testassert.Falsef(t, testassert.Any(
		func() bool { return response.Code != http.StatusAccepted },
		func() bool { return json.Unmarshal(response.Body.Bytes(), &summary) != nil },
	), "create reservation plan = %d %s", response.Code, response.Body.String())
	waitForEmulationStationMapping(t, handler, cookie, &summary)
	collections := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/emulationstation-imports/"+summary.ID+"/collections",
	)
	var page struct {
		Items []emulationstationimport.Collection `json:"items"`
	}
	if collections.Code != http.StatusOK || json.Unmarshal(collections.Body.Bytes(), &page) != nil ||
		len(page.Items) != 1 {
		t.Fatalf("reservation collections = %d %s", collections.Code, collections.Body.String())
	}
	mapEmulationStationHTTPCollection(t, handler, cookie, csrf, &summary, page.Items[0].ID, server)
	return startEmulationStationReviewPlan(t, handler, cookie, csrf, summary)
}

func startEmulationStationReviewPlan(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	summary emulationstationimport.Summary,
) emulationstationimport.Summary {
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
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &summary) != nil {
		t.Fatalf("start reservation plan = %d %s", response.Code, response.Body.String())
	}
	return summary
}

func waitForBlockedReviewHandoff(t *testing.T, server *Server, importID string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var sourceState string
		var retryable int
		err := server.database.QueryRowContext(context.Background(), `
SELECT execution_state,retryable
FROM emulationstation_import_items
WHERE import_id=?
`, importID).Scan(&sourceState, &retryable)
		if err == nil && sourceState == "COMMIT_FAILED" && retryable == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var itemID, itemState, handoffKind string
	if err := server.database.QueryRowContext(context.Background(), `
SELECT id,state,review_handoff_kind
FROM import_items
WHERE review_handoff_kind='EMULATIONSTATION'
`).Scan(&itemID, &itemState, &handoffKind); err != nil {
		t.Fatal(err)
	}
	var sourceState string
	var sourceItemID *string
	if err := server.database.QueryRowContext(context.Background(), `
SELECT execution_state,library_import_item_id
FROM emulationstation_import_items
WHERE import_id=?
`, importID).Scan(&sourceState, &sourceItemID); err != nil {
		t.Fatal(err)
	}
	var importJobs int
	if err := server.database.QueryRowContext(
		context.Background(),
		"SELECT count(*) FROM import_jobs",
	).Scan(&importJobs); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return itemState != "REVIEW_PENDING" },
		func() bool { return handoffKind != "EMULATIONSTATION" },
		func() bool { return sourceState != "COMMIT_FAILED" },
		func() bool { return sourceItemID != nil },
		func() bool { return importJobs != 1 },
	), "blocked handoff = item:%s/%s source:%s/%v imports:%d",
		itemState, handoffKind, sourceState, sourceItemID, importJobs)
	return itemID
}

func assertReviewHandoffBlocked(
	t *testing.T,
	server *Server,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	importID string,
	itemID string,
) {
	t.Helper()
	list := emulationStationGET(t, handler, cookie, "/api/v1/admin/reviews")
	testassert.Falsef(t, list.Code != http.StatusOK || strings.Contains(list.Body.String(), itemID),
		"reserved review list = %d %s", list.Code, list.Body.String())
	detail := emulationStationGET(t, handler, cookie, "/api/v1/admin/reviews/"+itemID)
	testassert.Falsef(t, detail.Code != http.StatusNotFound,
		"reserved review detail = %d %s", detail.Code, detail.Body.String())
	preview := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/review-bulk-approval-preview",
	)
	var bulk libraryimport.ReviewBulkPreview
	if preview.Code != http.StatusOK || json.Unmarshal(preview.Body.Bytes(), &bulk) != nil ||
		bulk.Counts.Matched != 0 {
		t.Fatalf("reserved review bulk preview = %d %s", preview.Code, preview.Body.String())
	}
	bulkCreateBody, _ := json.Marshal(libraryimport.ReviewBulkCreateRequest{
		Scope:                   bulk.Scope,
		ScopeDigest:             bulk.ScopeDigest,
		CandidateManifestDigest: bulk.CandidateManifestDigest,
	})
	bulkCreateRequest := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/admin/review-bulk-approvals",
		strings.NewReader(string(bulkCreateBody)),
	)
	bulkCreateRequest.Header.Set("Content-Type", "application/json")
	bulkCreateRequest.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(bulkCreateRequest, cookie, csrf)
	bulkCreate := httptest.NewRecorder()
	handler.ServeHTTP(bulkCreate, bulkCreateRequest)
	testassert.Falsef(t, bulkCreate.Code != http.StatusConflict ||
		!strings.Contains(bulkCreate.Body.String(), "REVIEW_BULK_SCOPE_EMPTY"),
		"reserved review bulk create = %d %s", bulkCreate.Code, bulkCreate.Body.String())
	var version int64
	if err := server.database.QueryRowContext(
		context.Background(),
		"SELECT version FROM review_drafts WHERE import_item_id=?",
		itemID,
	).Scan(&version); err != nil {
		t.Fatal(err)
	}
	approve := reviewDecisionRequest(t, handler, cookie, csrf, itemID, "approve", version)
	testassert.Falsef(t, approve.Code != http.StatusConflict,
		"reserved review approve = %d %s", approve.Code, approve.Body.String())
	discard := reviewDecisionRequest(t, handler, cookie, csrf, itemID, "discard", version)
	testassert.Falsef(t, discard.Code != http.StatusConflict,
		"reserved review discard = %d %s", discard.Code, discard.Body.String())
	filtered := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/reviews?emulationStationImportId="+importID,
	)
	testassert.Falsef(t, filtered.Code != http.StatusOK || strings.Contains(filtered.Body.String(), itemID),
		"reserved filtered review list = %d %s", filtered.Code, filtered.Body.String())
	assertReviewHandoffCounts(t, handler, cookie, 0)
}

func reviewDecisionRequest(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	itemID string,
	action string,
	version int64,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/admin/reviews/"+itemID+"/"+action,
		strings.NewReader(`{}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v`+jsonInt(version)+`"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func retryEmulationStationReviewHandoff(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	summary *emulationstationimport.Summary,
) {
	t.Helper()
	current := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/emulationstation-imports/"+summary.ID,
	)
	if current.Code != http.StatusOK || json.Unmarshal(current.Body.Bytes(), summary) != nil ||
		!summary.Retryable {
		t.Fatalf("retryable handoff summary = %d %s", current.Code, current.Body.String())
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/admin/emulationstation-imports/"+summary.ID+"/retry",
		strings.NewReader(`{}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v`+jsonInt(summary.Version)+`"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), summary) != nil {
		t.Fatalf("retry review handoff = %d %s", response.Code, response.Body.String())
	}
}

func waitForCompletedReviewHandoff(
	t *testing.T,
	server *Server,
	handler http.Handler,
	cookie *http.Cookie,
	summary *emulationstationimport.Summary,
	itemID string,
) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response := emulationStationGET(
			t,
			handler,
			cookie,
			"/api/v1/admin/emulationstation-imports/"+summary.ID,
		)
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), summary) == nil &&
			summary.State == "COMPLETED" && summary.Counts.ReviewPending == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	testassert.Falsef(t, summary.State != "COMPLETED" || summary.Counts.ReviewPending != 1,
		"completed review handoff = %#v", *summary)
	var attachedItemID, handoffKind string
	if err := server.database.QueryRowContext(context.Background(), `
SELECT source.library_import_item_id,item.review_handoff_kind
FROM emulationstation_import_items source
JOIN import_items item ON item.id=source.library_import_item_id
WHERE source.import_id=? AND source.execution_state='REVIEW_PENDING'
`, summary.ID).Scan(&attachedItemID, &handoffKind); err != nil {
		t.Fatal(err)
	}
	var importJobs int
	if err := server.database.QueryRowContext(
		context.Background(),
		"SELECT count(*) FROM import_jobs",
	).Scan(&importJobs); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return attachedItemID != itemID },
		func() bool { return handoffKind != "EMULATIONSTATION" },
		func() bool { return importJobs != 1 },
	), "attached handoff = item:%s/%s kind:%s imports:%d",
		attachedItemID, itemID, handoffKind, importJobs)
}

func assertReviewHandoffAvailable(
	t *testing.T,
	server *Server,
	handler http.Handler,
	cookie *http.Cookie,
	csrf string,
	importID string,
	itemID string,
) {
	t.Helper()
	assertReviewHandoffCounts(t, handler, cookie, 1)
	list := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/reviews?emulationStationImportId="+importID,
	)
	testassert.Falsef(t, list.Code != http.StatusOK || !strings.Contains(list.Body.String(), itemID),
		"attached review list = %d %s", list.Code, list.Body.String())
	detail := emulationStationGET(t, handler, cookie, "/api/v1/admin/reviews/"+itemID)
	var review struct {
		Version int64 `json:"version"`
	}
	if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &review) != nil {
		t.Fatalf("attached review detail = %d %s", detail.Code, detail.Body.String())
	}
	preview := emulationStationGET(
		t,
		handler,
		cookie,
		"/api/v1/admin/review-bulk-approval-preview?emulationStationImportId="+importID,
	)
	var bulk libraryimport.ReviewBulkPreview
	if preview.Code != http.StatusOK || json.Unmarshal(preview.Body.Bytes(), &bulk) != nil ||
		bulk.Counts.Matched != 1 || bulk.Counts.StrictReady != 1 {
		t.Fatalf("attached review bulk preview = %d %s", preview.Code, preview.Body.String())
	}
	approve := reviewDecisionRequest(t, handler, cookie, csrf, itemID, "approve", review.Version)
	if approve.Code != http.StatusCreated {
		t.Fatalf("attached review approve = %d %s", approve.Code, approve.Body.String())
	}
	var sourceState string
	var gameCount int
	if err := server.database.QueryRowContext(context.Background(), `
SELECT source.execution_state,(SELECT count(*) FROM games WHERE status='PUBLISHED')
FROM emulationstation_import_items source
WHERE source.import_id=?
`, importID).Scan(&sourceState, &gameCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, sourceState != "PUBLISHED" || gameCount != 1,
		"approved attached review = source:%s games:%d", sourceState, gameCount)
}

func assertReviewHandoffCounts(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	want int64,
) {
	t.Helper()
	homeResponse := emulationStationGET(t, handler, cookie, "/api/v1/home")
	var home struct {
		Imports struct {
			ReviewPendingCount int64 `json:"reviewPendingCount"`
		} `json:"imports"`
	}
	if homeResponse.Code != http.StatusOK || json.Unmarshal(homeResponse.Body.Bytes(), &home) != nil ||
		home.Imports.ReviewPendingCount != want {
		t.Fatalf("review handoff home count want %d = %d %s",
			want, homeResponse.Code, homeResponse.Body.String())
	}
	summaryResponse := emulationStationGET(t, handler, cookie, "/api/v1/admin/imports/summary")
	var summary struct {
		ReviewPending int64 `json:"reviewPending"`
	}
	if summaryResponse.Code != http.StatusOK ||
		json.Unmarshal(summaryResponse.Body.Bytes(), &summary) != nil ||
		summary.ReviewPending != want {
		t.Fatalf("review handoff import count want %d = %d %s",
			want, summaryResponse.Code, summaryResponse.Body.String())
	}
}
