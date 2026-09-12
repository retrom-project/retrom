//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/launch"
	"retrom/internal/testsupport"
)

const productCreateHTTPKey = "01980000-0000-7000-8000-000000000082"

func productCreateHTTP(t *testing.T, server *Server, gameID string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "01980000-0000-7000-8000-000000009999", ProfileID: "local"})
	body := fmt.Sprintf(`{"gameId":%q,"returnTo":"/library","clientCapabilities":{"secureContext":true,"crossOriginIsolated":true,"sharedArrayBuffer":true}}`, gameID)
	return productCreateHTTPBody(ctx, server, body)
}

func productCreateHTTPBody(ctx context.Context, server *Server, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/launches", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", productCreateHTTPKey)
	response := httptest.NewRecorder()
	server.createLaunch(response, request)
	return response
}

func TestProductCreateHTTPPendingReceiptRollsBackValidation(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, _ := seedMovableGame(t, server)
	var beforeInputs, beforeEvents int
	if err := server.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM job_input_snapshots),(SELECT count(*) FROM job_events)`).Scan(&beforeInputs, &beforeEvents); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(t.Context(), `UPDATE game_variants SET status='BLOCKED',compatibility_code='VALIDATION_PENDING',emulator_game_id=NULL WHERE game_id=?`, gameID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(t.Context(), `ALTER TABLE idempotency_records ADD COLUMN product_receipt_guard INTEGER CHECK(operation_id!='postLaunch')`); err != nil {
		t.Fatal(err)
	}
	response := productCreateHTTP(t, server, gameID)
	var jobs, inputs, events int
	err := server.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM jobs WHERE kind='VARIANT_VALIDATE'),(SELECT count(*) FROM job_input_snapshots),(SELECT count(*) FROM job_events)`).Scan(&jobs, &inputs, &events)
	launches, receipts := productCreateHTTPCounts(t, server)
	if err != nil || response.Code != http.StatusInternalServerError || launches != 0 || receipts != 0 || jobs != 0 || inputs != beforeInputs || events != beforeEvents || len(response.Result().Cookies()) != 0 {
		t.Fatalf("pending failure status=%d launches=%d receipts=%d jobs=%d inputs=%d events=%d cookies=%d error=%v", response.Code, launches, receipts, jobs, inputs, events, len(response.Result().Cookies()), err)
	}
}

func TestProductCreateHTTPReplaysPendingBytesAfterReady(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, _ := seedMovableGame(t, server)
	if _, err := server.database.ExecContext(t.Context(), `UPDATE game_variants SET status='BLOCKED',compatibility_code='VALIDATION_PENDING',emulator_game_id=NULL WHERE game_id=?`, gameID); err != nil {
		t.Fatal(err)
	}
	first := productCreateHTTP(t, server, gameID)
	var pending struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if first.Code != http.StatusAccepted || pending.JobID == "" || len(first.Result().Cookies()) != 0 {
		t.Fatalf("pending status=%d job=%q cookies=%d", first.Code, pending.JobID, len(first.Result().Cookies()))
	}
	if _, err := server.database.ExecContext(t.Context(), `UPDATE game_variants SET status='READY',compatibility_code='READY' WHERE game_id=?`, gameID); err != nil {
		t.Fatal(err)
	}
	replay := productCreateHTTP(t, server, gameID)
	if replay.Code != http.StatusAccepted || replay.Body.String() != first.Body.String() || replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" || len(replay.Result().Cookies()) != 0 {
		t.Fatalf("pending replay status=%d sameBody=%v replayHeader=%q cookies=%d", replay.Code, replay.Body.String() == first.Body.String(), replay.Header().Get("X-Retrom-Idempotent-Replay"), len(replay.Result().Cookies()))
	}
	conflict := productCreateHTTP(t, server, "different-game")
	launches, receipts := productCreateHTTPCounts(t, server)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "IDEMPOTENCY_KEY_REUSED") || launches != 0 || receipts != 1 {
		t.Fatalf("different request status=%d launches=%d receipts=%d", conflict.Code, launches, receipts)
	}
}

func productCreateHTTPCounts(t *testing.T, server *Server) (int, int) {
	t.Helper()
	var launches, receipts int
	if err := server.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM launch_sessions),(SELECT count(*) FROM idempotency_records WHERE operation_id='postLaunch')`).Scan(&launches, &receipts); err != nil {
		t.Fatal(err)
	}
	return launches, receipts
}

func TestProductCreateHTTPReceiptFailureRollsBackLaunch(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, _ := seedMovableGame(t, server)
	if _, err := server.database.ExecContext(t.Context(), `ALTER TABLE idempotency_records ADD COLUMN product_receipt_guard INTEGER CHECK(operation_id!='postLaunch')`); err != nil {
		t.Fatal(err)
	}
	response := productCreateHTTP(t, server, gameID)
	launches, receipts := productCreateHTTPCounts(t, server)
	if response.Code != http.StatusInternalServerError || launches != 0 || receipts != 0 || len(response.Result().Cookies()) != 0 {
		t.Fatalf("receipt failure status=%d launches=%d receipts=%d cookies=%d", response.Code, launches, receipts, len(response.Result().Cookies()))
	}
}

func TestProductCreateHTTPConcurrentServersShareOneReceipt(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, _ := seedMovableGame(t, server)
	now := time.Now()
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var calls atomic.Int32
	clock := func() time.Time {
		if calls.Add(1) <= 2 {
			ready <- struct{}{}
			<-release
		}
		return now
	}
	builder, err := testsupport.NewRuntimeBuilder(t.Context(), server.database)
	if err != nil {
		t.Fatal(err)
	}
	launcher := launch.New(server.database, server.dependencies, server.credentials, clock).WithBlobStore(server.blobs).WithRuntimeProvider(server.dependencies.RuntimeCatalog, builder)
	servers := []*Server{
		{database: server.database, credentials: server.credentials, config: server.config, launcher: launcher, now: func() time.Time { return now }},
		{database: server.database, credentials: server.credentials, config: server.config, launcher: launcher, now: func() time.Time { return now }},
	}
	outcomes := make(chan *httptest.ResponseRecorder, 2)
	for _, current := range servers {
		go func() { outcomes <- productCreateHTTP(t, current, gameID) }()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("product clock barrier was not reached")
		}
	}
	unblock()
	first, second := <-outcomes, <-outcomes
	launches, receipts := productCreateHTTPCounts(t, server)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated || launches != 1 || receipts != 1 || first.Body.String() != second.Body.String() {
		t.Fatalf("concurrent status=%d/%d launches=%d receipts=%d sameBody=%v", first.Code, second.Code, launches, receipts, first.Body.String() == second.Body.String())
	}
	firstReplayed := first.Header().Get("X-Retrom-Idempotent-Replay") == "true"
	secondReplayed := second.Header().Get("X-Retrom-Idempotent-Replay") == "true"
	if firstReplayed == secondReplayed || len(first.Result().Cookies()) != 2 || len(second.Result().Cookies()) != 2 {
		t.Fatalf("concurrent replay headers=%v/%v cookies=%d/%d", firstReplayed, secondReplayed, len(first.Result().Cookies()), len(second.Result().Cookies()))
	}
}
