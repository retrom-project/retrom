//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

func TestReviewApprovalHTTPSkipsExistingAndReplaysResult(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	first := createReviewSnapshotItem(t, server)
	second := createReviewSnapshotItem(t, server)
	stale := requestReviewApprove(t, server, first, `"v2"`, `{}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale=%d %s", stale.Code, stale.Body.String())
	}
	response := requestReviewApprove(t, server, first, `"v1"`, `{"reason":"发布此游戏"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("first=%d %s", response.Code, response.Body.String())
	}
	var published libraryservice.ReviewApproved
	if err := json.Unmarshal(response.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published.GameID == "" || published.Status != "PUBLISHED" {
		t.Fatalf("published=%+v", published)
	}
	duplicate := requestReviewApprove(t, server, second, `"v1"`, `{}`)
	if duplicate.Code != http.StatusCreated {
		t.Fatalf("duplicate=%d %s", duplicate.Code, duplicate.Body.String())
	}
	assertApprovalDuplicateResponse(t, duplicate, published.GameID)
	repeat := requestReviewApprove(t, server, second, `"v1"`, `{}`)
	if repeat.Code != http.StatusCreated {
		t.Fatalf("repeat=%d %s", repeat.Code, repeat.Body.String())
	}
	var count int
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT count(*) FROM games`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate skip/replay created %d games", count)
	}
}

func assertApprovalDuplicateResponse(t *testing.T, duplicate *httptest.ResponseRecorder, gameID string) {
	t.Helper()
	var result libraryservice.ReviewApproved
	if err := json.Unmarshal(duplicate.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.GameID != gameID || result.Status != "SKIPPED_EXISTING" {
		t.Fatalf("duplicate response=%s", duplicate.Body.String())
	}
}
