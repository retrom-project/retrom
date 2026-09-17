//go:build integration

package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	application "retrom/internal/model/libraryimport"
)

func TestReviewApprovalHTTPKeepsSuccessConflictAndDuplicateContracts(t *testing.T) {
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
	var published application.ReviewApproved
	if err := json.Unmarshal(response.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published.GameID == "" || published.EventID == "" || published.Status != "PUBLISHED" {
		t.Fatalf("published=%+v", published)
	}
	duplicate := requestReviewApprove(t, server, second, `"v1"`, `{}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate=%d %s", duplicate.Code, duplicate.Body.String())
	}
	assertApprovalDuplicateResponse(t, duplicate, published.GameID)
	confirmed := requestReviewApprove(t, server, second, `"v1"`, fmt.Sprintf(`{"duplicatePolicy":"ALLOW_NEW","acknowledgedGameIds":[%q]}`, published.GameID))
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirmed=%d %s", confirmed.Code, confirmed.Body.String())
	}
	repeat := requestReviewApprove(t, server, second, `"v1"`, `{}`)
	if repeat.Code != http.StatusConflict {
		t.Fatalf("repeat=%d %s", repeat.Code, repeat.Body.String())
	}
	var count int
	if err := server.database.QueryRowContext(t.Context(), `SELECT count(*) FROM games`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("duplicate confirmation/replay created %d games", count)
	}
}

func assertApprovalDuplicateResponse(t *testing.T, duplicate *httptest.ResponseRecorder, gameID string) {
	t.Helper()
	var conflict struct {
		Error struct {
			Code    string
			Details application.DuplicateConflict
		}
	}
	if err := json.Unmarshal(duplicate.Body.Bytes(), &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.Error.Code != "DUPLICATE_GAME_CONFIRMATION_REQUIRED" || len(conflict.Error.Details.Games) != 1 || conflict.Error.Details.Games[0].GameID != gameID || len(conflict.Error.Details.ContentIdentityDigest) != 64 {
		t.Fatalf("duplicate response=%s", duplicate.Body.String())
	}
}
