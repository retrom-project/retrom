//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/launch"
	"retrom/internal/payloadrelease"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestGameMovePreviewQueuesTargetCoreValidationAndPreservesHistory(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	gameID, contentID := seedMovableGame(t, server)
	targetID := "01980000-0000-7000-8000-000000000171"
	if _, err := server.database.ExecContext(ctx, `
INSERT INTO platform_instances(id,
platform_id,
default_core_id,
name,
slug,
description,
sort_order,
enabled,
version,
created_at_ms,
updated_at_ms) VALUES(?,
'gbc',
'mgba',
'Move target',
'move-target',
'',
99,
1,
1,
?,
?)
`, targetID, time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	handler := server.Handler()
	cookie, csrfToken := testSessionCredentials()
	previewBody := fmt.Sprintf(`{"targetPlatformInstanceId":%q}`, targetID)
	send := func(path, body, key, etag string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		request.Header.Set("If-Match", etag)
		setCSRFCredentials(request, cookie, csrfToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	keys := []string{
		"01980000-0000-7000-8000-000000000172",
		"01980000-0000-7000-8000-000000000173",
	}
	responses := make([]*httptest.ResponseRecorder, len(keys))
	var wait sync.WaitGroup
	server.idempotency.Lock()
	idempotencyLocked := true
	defer func() {
		if idempotencyLocked {
			server.idempotency.Unlock()
		}
	}()
	for index := range keys {
		wait.Add(1)
		go func() {
			defer wait.Done()
			responses[index] = send("/api/v1/admin/games/"+gameID+"/move-preview", previewBody, keys[index], `"v1"`)
		}()
	}
	waitForIdempotencyQueue(t, server, len(keys))
	server.idempotency.Unlock()
	idempotencyLocked = false
	wait.Wait()
	jobIDs := make([]string, len(responses))
	for index, response := range responses {
		var payload struct {
			Status string `json:"status"`
			JobID  string `json:"jobId"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil ||
			response.Code != http.StatusAccepted || payload.Status != "VALIDATION_PENDING" || payload.JobID == "" {
			t.Fatalf("move preview %d = %d %s, error=%v", index, response.Code, response.Body.String(), err)
		}
		jobIDs[index] = payload.JobID
	}
	testassert.Falsef(t, jobIDs[0] != jobIDs[1], "concurrent move previews queued different jobs: %v", jobIDs)
	waitForHTTPJob(t, server.database, jobIDs[0], "SUCCEEDED")

	replayed := send("/api/v1/admin/games/"+gameID+"/move-preview", previewBody, keys[0], `"v1"`)
	testassert.Falsef(t, testassert.Any(func() bool { return replayed.Code != http.StatusAccepted }, func() bool { return replayed.Body.String() != responses[0].Body.String() }), "old preview key was not replayed: %d %s", replayed.Code, replayed.Body.String())
	ready := send(
		"/api/v1/admin/games/"+gameID+"/move-preview",
		previewBody,
		"01980000-0000-7000-8000-000000000174",
		`"v1"`,
	)
	var readyBody struct {
		Impact struct {
			VariantStatus string `json:"variantStatus"`
		} `json:"impact"`
		ImpactDigest string `json:"impactDigest"`
	}
	if err := json.Unmarshal(ready.Body.Bytes(), &readyBody); err != nil || ready.Code != http.StatusOK ||
		readyBody.Impact.VariantStatus != "READY" || readyBody.ImpactDigest == "" {
		t.Fatalf("ready move preview = %d %s, error=%v", ready.Code, ready.Body.String(), err)
	}
	commitBody := fmt.Sprintf(
		`{"targetPlatformInstanceId":%q,"impactDigest":%q,"confirmBlocked":false}`,
		targetID,
		readyBody.ImpactDigest,
	)
	committed := send(
		"/api/v1/admin/games/"+gameID+"/move",
		commitBody,
		"01980000-0000-7000-8000-000000000175",
		`"v1"`,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return committed.Code != http.StatusOK }, func() bool { return committed.Header().Get("ETag") != `"v2"` }), "move commit = %d %s", committed.Code, committed.Body.String())
	var storedTarget, storedContent string
	var version, variantCount, revisionCount, auditCount int64
	if err := server.database.QueryRowContext(ctx, `
SELECT platform_instance_id,
current_content_revision_id,
version,
(SELECT count(*) FROM game_variants WHERE game_id=games.id),
(SELECT count(*) FROM game_variant_revisions r JOIN game_variants v ON v.id=r.game_variant_id WHERE v.game_id=games.id),
(SELECT count(*) FROM audit_events WHERE resource_type='GAME' AND resource_id=games.id AND action='GAME_MOVED')
FROM games
WHERE id=?
`, gameID).Scan(&storedTarget, &storedContent, &version, &variantCount, &revisionCount, &auditCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return storedTarget != targetID }, func() bool { return storedContent != contentID }, func() bool { return version != 2 }, func() bool { return variantCount != 2 }, func() bool { return revisionCount != 2 }, func() bool { return auditCount != 1 }), "move state = target:%s content:%s version:%d variants:%d revisions:%d audits:%d", storedTarget, storedContent, version, variantCount, revisionCount, auditCount)
}

func waitForIdempotencyQueue(t *testing.T, server *Server, expected int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		server.idempotencyQueueMu.Lock()
		waiters := server.idempotencyQueueWaiters
		server.idempotencyQueueMu.Unlock()
		if waiters == expected {
			return
		}
		testassert.Falsef(t, time.Now().After(deadline), "idempotency queue waiters = %d, want %d", waiters, expected)
		time.Sleep(time.Millisecond)
	}
}

func TestPlatformInstanceVisibilityAndNonEmptyDeletionBoundaries(t *testing.T) {
	server := newTestServer(t)
	if err := server.dependencies.Bootstrap(context.Background(), server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(context.Background(), server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	gameID, _ := seedMovableGame(t, server)
	handler := server.Handler()
	cookie, csrfToken := testSessionCredentials()
	send := func(method, path, body, version string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		request.Header.Set("If-Match", version)
		setCSRFCredentials(request, cookie, csrfToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	sourceID := testsupport.MustPlatformInstanceID(t, server.database, "gbc/gambatte")
	disabled := send(http.MethodPatch, "/api/v1/admin/platform-instances/"+sourceID, `{"enabled":false}`, `"v1"`)
	testassert.Falsef(t, testassert.Any(func() bool { return disabled.Code != http.StatusOK }, func() bool { return disabled.Header().Get("ETag") != `"v2"` }), "disable non-empty platform = %d %s", disabled.Code, disabled.Body.String())
	userGames := httptest.NewRecorder()
	handler.ServeHTTP(userGames, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games?limit=100", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return userGames.Code != http.StatusOK }, func() bool { return strings.Contains(userGames.Body.String(), gameID) }), "disabled platform leaked into user games = %d %s", userGames.Code, userGames.Body.String())
	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/home", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return home.Code != http.StatusOK }, func() bool { return !strings.Contains(home.Body.String(), `"gameCount":0`) }), "disabled platform leaked into home = %d %s", home.Code, home.Body.String())
	adminGames := httptest.NewRecorder()
	handler.ServeHTTP(adminGames, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/games?limit=100", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return adminGames.Code != http.StatusOK }, func() bool { return !strings.Contains(adminGames.Body.String(), gameID) }), "disabled platform missing from admin games = %d %s", adminGames.Code, adminGames.Body.String())
	gameDetail := httptest.NewRecorder()
	handler.ServeHTTP(gameDetail, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games/"+gameID, nil))
	testassert.Falsef(t, gameDetail.Code != http.StatusNotFound, "disabled platform game detail = %d %s", gameDetail.Code, gameDetail.Body.String())
	if _, err := server.launcher.Create(context.Background(), "local", launch.CreateRequest{
		GameID: gameID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: launch.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true},
	}); !errors.Is(err, launch.ErrBlocked) {
		t.Fatalf("disabled platform launch error = %v", err)
	}
	deleted := send(http.MethodDelete, "/api/v1/admin/platform-instances/"+sourceID, "", `"v2"`)
	testassert.Falsef(t, testassert.Any(func() bool { return deleted.Code != http.StatusConflict }, func() bool { return !strings.Contains(deleted.Body.String(), `"code":"PLATFORM_INSTANCE_NOT_EMPTY"`) }), "delete non-empty platform = %d %s", deleted.Code, deleted.Body.String())
	reenabled := send(http.MethodPatch, "/api/v1/admin/platform-instances/"+sourceID, `{"enabled":true}`, `"v2"`)
	testassert.Falsef(t, testassert.Any(func() bool { return reenabled.Code != http.StatusOK }, func() bool { return reenabled.Header().Get("ETag") != `"v3"` }), "re-enable non-empty platform = %d %s", reenabled.Code, reenabled.Body.String())
	restoredGames := httptest.NewRecorder()
	handler.ServeHTTP(restoredGames, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games?limit=100", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return restoredGames.Code != http.StatusOK }, func() bool { return !strings.Contains(restoredGames.Body.String(), gameID) }), "re-enabled platform missing from user games = %d %s", restoredGames.Code, restoredGames.Body.String())

	var ownerID string
	if err := server.database.QueryRowContext(context.Background(), `SELECT platform_instance_id FROM games WHERE id=?`, gameID).Scan(&ownerID); err != nil ||
		ownerID != sourceID {
		t.Fatalf("game owner = %s, error=%v", ownerID, err)
	}
	if _, err := server.database.ExecContext(context.Background(), `UPDATE games SET platform_instance_id=NULL WHERE id=?`, gameID); err == nil {
		t.Fatal("published game accepted an empty platform instance owner")
	}
	columns, err := server.database.QueryContext(context.Background(), `PRAGMA table_info(games)`)
	testassert.False(t, err != nil, err)
	defer func() { cleanup.Error("close", columns.Close()) }()
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := columns.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		testassert.False(t, name == "platform_id", "games table exposes a second direct platform owner")
	}
	if err := columns.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultCoreImpactPaginationRejectsDriftAndPreservesSaveLaunch(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	gameID, _ := seedMovableGame(t, server)
	cloneMovableGame(t, server, gameID, "181", "182", "183", "184", "185")
	cloneMovableGame(t, server, gameID, "186", "187", "188", "189", "190")
	capabilities := launch.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	sourceLaunch, err := server.launcher.Create(
		ctx,
		"local",
		launch.CreateRequest{GameID: gameID, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return sourceLaunch.LaunchID == "" }), "source launch = %#v, error=%v", sourceLaunch, err)

	handler := server.Handler()
	cookie, csrfToken := testSessionCredentials()
	instanceID := testsupport.MustPlatformInstanceID(t, server.database, "gbc/gambatte")
	preview := func(cursorValue *string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"coreId": "mgba", "cursor": cursorValue, "limit": 1})
		testassert.False(t, err != nil, err)
		request := httptest.NewRequestWithContext(context.Background(),
			http.MethodPost,
			"/api/v1/admin/platform-instances/"+instanceID+"/default-core-preview",
			bytes.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"v1"`)
		request.Header.Set("Idempotency-Key", uuid.NewString())
		setCSRFCredentials(request, cookie, csrfToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	type previewResponse struct {
		Counts map[string]int64 `json:"counts"`
		Items  []struct {
			GameID string `json:"gameId"`
		} `json:"items"`
		NextCursor              *string `json:"nextCursor"`
		ImpactDigest            string  `json:"impactDigest"`
		PlatformInstanceVersion int64   `json:"platformInstanceVersion"`
	}
	first := preview(nil)
	var firstBody previewResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil || first.Code != http.StatusOK ||
		len(firstBody.Items) != 1 || firstBody.NextCursor == nil || firstBody.Counts["needsValidation"] != 3 {
		t.Fatalf("first default core page = %d %s, error=%v", first.Code, first.Body.String(), err)
	}
	oldCursor := *firstBody.NextCursor
	newMetadataID := "01980000-0000-7000-8000-000000000192"
	if _, err := server.database.ExecContext(ctx, `
INSERT INTO game_metadata_revisions(id,
game_id,
title,
title_initial,
description,
developer,
publisher,
genre,
players,
release_year,
source_kind,
source_ref_id,
created_at_ms)
SELECT ?,
game_id,
title,
title_initial,
description,
developer,
publisher,
genre,
players,
release_year,
'ADMIN_EDIT',
NULL,
?
FROM game_metadata_revisions
WHERE id=(SELECT current_metadata_revision_id FROM games WHERE id=?)
`, newMetadataID, time.Now().UnixMilli(), gameID); err != nil {
		t.Fatal(err)
	}
	var originalMetadataID string
	if err := server.database.QueryRowContext(ctx, `SELECT current_metadata_revision_id FROM games WHERE id=?`, gameID).
		Scan(&originalMetadataID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(ctx, `UPDATE games SET current_metadata_revision_id=? WHERE id=?`, newMetadataID, gameID); err != nil {
		t.Fatal(err)
	}
	stale := preview(&oldCursor)
	testassert.Falsef(t, testassert.Any(func() bool { return stale.Code != http.StatusConflict }, func() bool { return !strings.Contains(stale.Body.String(), `"code":"IMPACT_PREVIEW_STALE"`) }), "drifted preview cursor = %d %s", stale.Code, stale.Body.String())
	if _, err := server.database.ExecContext(ctx, `UPDATE games SET current_metadata_revision_id=? WHERE id=?`, originalMetadataID, gameID); err != nil {
		t.Fatal(err)
	}

	seen := map[string]struct{}{}
	var cursorValue *string
	var digest string
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		response := preview(cursorValue)
		var payload previewResponse
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != http.StatusOK ||
			len(payload.Items) != 1 || payload.Counts["needsValidation"] != 3 || payload.PlatformInstanceVersion != 1 {
			t.Fatalf("default core page %d = %d %s, error=%v", pageNumber, response.Code, response.Body.String(), err)
		}
		if digest == "" {
			digest = payload.ImpactDigest
		} else {
			testassert.Falsef(t, payload.ImpactDigest != digest, "impact digest drifted across pages: %s != %s", payload.ImpactDigest, digest)
		}
		if _, duplicate := seen[payload.Items[0].GameID]; duplicate {
			t.Fatalf("duplicate game across preview pages: %s", payload.Items[0].GameID)
		}
		seen[payload.Items[0].GameID] = struct{}{}
		cursorValue = payload.NextCursor
	}
	testassert.Falsef(t, testassert.Any(func() bool { return len(seen) != 3 }, func() bool { return cursorValue != nil }), "preview coverage = %d games, cursor=%v", len(seen), cursorValue)

	requestBody := fmt.Sprintf(`{"coreId":"mgba","impactDigest":%q,"confirmBlocked":false}`, digest)
	request := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost,
		"/api/v1/admin/platform-instances/"+instanceID+"/default-core",
		strings.NewReader(requestBody),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"v1"`)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	setCSRFCredentials(request, cookie, csrfToken)
	changed := httptest.NewRecorder()
	handler.ServeHTTP(changed, request)
	testassert.Falsef(t, testassert.Any(func() bool { return changed.Code != http.StatusOK }, func() bool { return changed.Header().Get("ETag") != `"v2"` }), "default core change = %d %s", changed.Code, changed.Body.String())

	saveID := "01980000-0000-7000-8000-000000000191"
	seedProductSave(t, server.database, saveID, sourceLaunch.LaunchID, "Old core save")
	pending, err := server.launcher.Create(
		ctx,
		"local",
		launch.CreateRequest{GameID: gameID, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return pending.Status != "VALIDATION_PENDING" }, func() bool { return pending.JobID == "" }), "new default core launch = %#v, error=%v", pending, err)
	saved, err := server.launcher.Create(
		ctx,
		"local",
		launch.CreateRequest{
			GameID: gameID, SaveStateID: &saveID, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities,
		},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return saved.LaunchID == "" }, func() bool { return saved.Status == "VALIDATION_PENDING" }), "old save launch = %#v, error=%v", saved, err)
	var savedCore string
	if err := server.database.QueryRowContext(ctx, `
SELECT a.core_id
FROM launch_sessions l
JOIN core_artifacts a ON a.id=l.core_artifact_id
WHERE l.id=?
`, saved.LaunchID).Scan(&savedCore); err != nil || savedCore != "gambatte" {
		t.Fatalf("save launch core = %s, error=%v", savedCore, err)
	}
}

func TestGameMetadataRevisionProjectionAndOptimisticEdit(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, contentID := seedMovableGame(t, server)
	handler, cookie, csrf := httpSession(t, server)

	detail := httptest.NewRecorder()
	detailRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/games/"+gameID, nil)
	handler.ServeHTTP(detail, detailRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return detail.Code != http.StatusOK }, func() bool { return detail.Header().Get("ETag") != `"v1"` }, func() bool { return !strings.Contains(detail.Body.String(), `"contentRevisions"`) }, func() bool { return !strings.Contains(detail.Body.String(), `"variants"`) }), "admin game projection = %d %s", detail.Code, detail.Body.String())

	sendPatch := func(etag string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(),
			http.MethodPatch,
			"/api/v1/admin/games/"+gameID,
			strings.NewReader(
				`{"title":"打击者1945","description":"Updated","developer":"Retrom","publisher":"Local","genre":"Puzzle","players":2,"releaseYear":2001}`,
			),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", etag)
		setCSRFCredentials(request, cookie, csrf)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	edited := sendPatch(`"v1"`)
	testassert.Falsef(t, testassert.Any(func() bool { return edited.Code != http.StatusOK }, func() bool { return edited.Header().Get("ETag") != `"v2"` }), "metadata edit = %d %s", edited.Code, edited.Body.String())
	stale := sendPatch(`"v1"`)
	testassert.Falsef(t, testassert.Any(func() bool { return stale.Code != http.StatusConflict }, func() bool { return !strings.Contains(stale.Body.String(), `"code":"VERSION_CONFLICT"`) }), "stale metadata edit = %d %s", stale.Code, stale.Body.String())
	var title, titleInitial, previousTitleInitial, sourceKind string
	var sourceRef sql.NullString
	var storedContent, ownerID string
	var auditCount, revisionCount int64
	if err := server.database.QueryRowContext(context.Background(), `
SELECT m.title,
m.title_initial,
(SELECT previous.title_initial FROM game_metadata_revisions previous
 WHERE previous.game_id=g.id AND previous.id<>g.current_metadata_revision_id),
m.source_kind,
m.source_ref_id,
g.current_content_revision_id,
g.platform_instance_id,
(SELECT count(*) FROM game_metadata_revisions WHERE game_id=g.id),
(SELECT count(*) FROM audit_events WHERE resource_type='GAME' AND resource_id=g.id AND action='GAME_METADATA_UPDATED')
FROM games g
JOIN game_metadata_revisions m ON m.id=g.current_metadata_revision_id
WHERE g.id=?
`, gameID).Scan(
		&title, &titleInitial, &previousTitleInitial, &sourceKind, &sourceRef,
		&storedContent, &ownerID, &revisionCount, &auditCount,
	); err != nil {
		t.Fatal(err)
	}
	gbcID := testsupport.MustPlatformInstanceID(t, server.database, "gbc/gambatte")
	testassert.Falsef(t, testassert.Any(
		func() bool { return title != "打击者1945" },
		func() bool { return titleInitial != "D" },
		func() bool { return previousTitleInitial != "M" },
		func() bool { return sourceKind != "ADMIN_EDIT" },
		func() bool { return sourceRef.Valid },
		func() bool { return storedContent != contentID },
		func() bool { return ownerID != gbcID },
		func() bool { return revisionCount != 2 },
		func() bool { return auditCount != 1 },
	), "metadata state = title:%s initial:%s/%s source:%s/%v content:%s owner:%s revisions:%d audits:%d",
		title, titleInitial, previousTitleInitial, sourceKind, sourceRef, storedContent, ownerID, revisionCount, auditCount)
	public := httptest.NewRecorder()
	handler.ServeHTTP(public, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games/"+gameID, nil))
	testassert.Falsef(t, testassert.Any(func() bool { return public.Code != http.StatusOK }, func() bool { return !strings.Contains(public.Body.String(), `"title":"打击者1945"`) }), "public game metadata = %d %s", public.Code, public.Body.String())
}

func TestGamePermanentDeleteIsIdempotentReleasesPayloadAndPreservesTombstone(t *testing.T) {
	server := newReadyHTTPServer(t)
	gameID, _ := seedMovableGame(t, server)
	cloneMovableGame(t, server, gameID, "194", "195", "196", "197", "198")
	sharedGameID := "01980000-0000-7000-8000-000000000194"
	ctx := context.Background()
	const historyUserID = "01980000-0000-7000-8000-000000009996"
	const historyProfileID = "01980000-0000-7000-8000-000000009997"
	if _, err := server.database.ExecContext(ctx,
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,'Payload history player',0)`,
		historyProfileID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(ctx, `
INSERT INTO users(id,profile_id,username,display_name,role,status,session_version,version,created_at_ms,updated_at_ms)
VALUES(?,?,'payload-history-admin','Payload History Admin','ADMIN','ENABLED',1,1,0,0)
`, historyUserID, historyProfileID); err != nil {
		t.Fatal(err)
	}
	server.authenticator = fixedAuthenticator{Principal: authn.Principal{
		UserID: historyUserID, ProfileID: historyProfileID, Username: "payload-history-admin",
		DisplayName: "Payload History Admin", Role: "ADMIN", SessionID: "01980000-0000-7000-8000-000000009995",
	}}
	created, err := server.launcher.Create(
		ctx,
		historyProfileID,
		launch.CreateRequest{
			GameID:   gameID,
			ReturnTo: "/games/" + gameID,
			ClientCapabilities: launch.Capabilities{
				SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
			},
		},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return created.LaunchID == "" }), "create launch before deletion = %#v, error=%v", created, err)
	launchConfiguration, err := server.launcher.Config(ctx, created.LaunchID, created.Capability)
	testassert.False(t, err != nil, err)
	var revisionID, artifactID, blobID string
	if err := server.database.QueryRowContext(ctx, `
SELECT r.id,
r.core_artifact_id,
f.blob_id
FROM games g
JOIN game_variants v ON v.game_id=g.id AND v.core_id='gambatte'
JOIN game_variant_revisions r ON r.id=v.current_revision_id
JOIN game_content_files f ON f.game_content_revision_id=g.current_content_revision_id AND f.role='CONTENT'
WHERE g.id=?
`, gameID).Scan(&revisionID, &artifactID, &blobID); err != nil {
		t.Fatal(err)
	}
	saveID := "01980000-0000-7000-8000-000000000193"
	seedProductSave(t, server.database, saveID, created.LaunchID, "Delete fixture save")
	if _, err := server.database.ExecContext(ctx, `
INSERT INTO play_sessions(id,launch_session_id,profile_id,game_id,game_variant_revision_id,
started_at_ms,last_heartbeat_at_ms,active_duration_ms,last_client_sequence,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,(SELECT profile_id FROM launch_sessions WHERE id=?),?,?,?, ?,60000,0,'ACTIVE',1,?,?)
`, "01980000-0000-7000-8000-000000000192", created.LaunchID, created.LaunchID, gameID,
		revisionID, time.Now().UnixMilli(), time.Now().UnixMilli(), time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(ctx, `
INSERT INTO favorite_games(profile_id,game_id,created_at_ms)
SELECT profile_id,?,? FROM launch_sessions WHERE id=?
`, gameID, time.Now().UnixMilli(), created.LaunchID); err != nil {
		t.Fatal(err)
	}
	handler, cookie, csrf := httpSession(t, server)
	runtimeGrant := &http.Cookie{
		Name: runtimeContentGrantPrefix + created.LaunchID, Value: created.Capability, Path: "/runtime/content/",
	}
	beforeDeleteRequest := httptest.NewRequestWithContext(ctx, http.MethodGet, launchConfiguration.GameURL, nil)
	beforeDeleteRequest.AddCookie(runtimeGrant)
	beforeDelete := httptest.NewRecorder()
	handler.ServeHTTP(beforeDelete, beforeDeleteRequest)
	testassert.Falsef(t, beforeDelete.Code != http.StatusOK,
		"runtime content before delete = %d %s", beforeDelete.Code, beforeDelete.Body.String())
	impact, err := payloadrelease.GameDeleteImpact(ctx, server.database, gameID)
	if err != nil {
		t.Fatal(err)
	}
	secondSaveID := "01980000-0000-7000-8000-000000000199"
	seedProductSave(t, server.database, secondSaveID, created.LaunchID, "Concurrent save")
	sendDelete := func(targetID, etag, title, digest, key string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(),
			http.MethodDelete,
			"/api/v1/admin/games/"+targetID,
			strings.NewReader(fmt.Sprintf(`{"confirmTitle":%q,"impactDigest":%q}`, title, digest)),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", etag)
		request.Header.Set("Idempotency-Key", key)
		setCSRFCredentials(request, cookie, csrf)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	if response := sendDelete(gameID, `"v1"`, "Move fixture", impact.ImpactDigest, uuid.NewString()); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"GAME_DELETE_IMPACT_STALE"`) {
		t.Fatalf("stale impact delete = %d %s", response.Code, response.Body.String())
	}
	impact, err = payloadrelease.GameDeleteImpact(ctx, server.database, gameID)
	if err != nil || impact.SharedBytes == "0" {
		t.Fatalf("shared game impact = %#v, error=%v", impact, err)
	}
	if response := sendDelete(gameID, `"v2"`, "Move fixture", impact.ImpactDigest, uuid.NewString()); response.Code != http.StatusConflict {
		t.Fatalf("stale game delete = %d %s", response.Code, response.Body.String())
	}
	if response := sendDelete(gameID, `"v1"`, "Wrong title", impact.ImpactDigest, uuid.NewString()); response.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(response.Body.String(), `"code":"GAME_DELETE_CONFIRMATION_MISMATCH"`) {
		t.Fatalf("mismatched game delete = %d %s", response.Code, response.Body.String())
	}
	key := uuid.NewString()
	deleted := sendDelete(gameID, `"v1"`, "Move fixture", impact.ImpactDigest, key)
	testassert.Falsef(t, testassert.Any(func() bool { return deleted.Code != http.StatusAccepted }, func() bool { return deleted.Header().Get("ETag") != `"v2"` }, func() bool { return !strings.Contains(deleted.Body.String(), `"payloadState":"RELEASING"`) }), "game delete = %d %s", deleted.Code, deleted.Body.String())
	replayed := sendDelete(gameID, `"v1"`, "Move fixture", impact.ImpactDigest, key)
	testassert.Falsef(t, testassert.Any(func() bool { return replayed.Code != http.StatusAccepted }, func() bool { return replayed.Header().Get("X-Retrom-Idempotent-Replay") != "true" }, func() bool { return replayed.Body.String() != deleted.Body.String() }), "game delete replay = %d %s", replayed.Code, replayed.Body.String())
	again := sendDelete(gameID, `"v2"`, "Move fixture", impact.ImpactDigest, uuid.NewString())
	testassert.Falsef(t, testassert.Any(func() bool { return again.Code != http.StatusOK }, func() bool { return !strings.Contains(again.Body.String(), `"status":"DELETED"`) }), "second game delete = %d %s", again.Code, again.Body.String())
	deadline := time.Now().Add(3 * time.Second)
	for {
		var payloadState string
		if err := server.database.QueryRowContext(ctx, `SELECT payload_state FROM games WHERE id=?`, gameID).Scan(&payloadState); err != nil {
			t.Fatal(err)
		}
		if payloadState == "RELEASED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("game payload state = %s", payloadState)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var status, payloadState, launchState string
	var deletedAt sql.NullInt64
	var version, saveCount, metadataCount, contentCount, contentFileCount, variantCount, variantFileCount, auditCount int64
	if err := server.database.QueryRowContext(ctx, `
SELECT g.status,
g.payload_state,
g.deleted_at_ms,
g.version,
(SELECT count(*) FROM save_states WHERE game_id=g.id),
(SELECT count(*) FROM game_metadata_revisions WHERE game_id=g.id),
(SELECT count(*) FROM game_content_revisions WHERE game_id=g.id),
(SELECT count(*) FROM game_content_files file JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id WHERE revision.game_id=g.id),
(SELECT count(*) FROM game_variant_revisions r JOIN game_variants v ON v.id=r.game_variant_id WHERE v.game_id=g.id),
(SELECT count(*) FROM variant_files file JOIN game_variant_revisions revision ON revision.id=file.game_variant_revision_id JOIN game_variants variant ON variant.id=revision.game_variant_id WHERE variant.game_id=g.id),
(SELECT count(*) FROM audit_events WHERE resource_type='GAME' AND resource_id=g.id AND action='GAME_PERMANENT_DELETE_REQUESTED'),
(SELECT state FROM launch_sessions WHERE id=?)
FROM games g
WHERE g.id=?
`, created.LaunchID, gameID).Scan(
		&status,
		&payloadState,
		&deletedAt,
		&version,
		&saveCount,
		&metadataCount,
		&contentCount,
		&contentFileCount,
		&variantCount,
		&variantFileCount,
		&auditCount,
		&launchState,
	); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return status != "DELETED" }, func() bool { return payloadState != "RELEASED" }, func() bool { return !deletedAt.Valid }, func() bool { return version != 2 }, func() bool { return saveCount != 0 }, func() bool { return metadataCount != 1 }, func() bool { return contentCount != 1 }, func() bool { return contentFileCount != 0 }, func() bool { return variantCount != 1 }, func() bool { return variantFileCount != 0 }, func() bool { return auditCount != 1 }, func() bool { return launchState != "REVOKED" }), "deleted aggregate = %s/%s/%v v%d saves:%d metadata:%d content:%d/%d variants:%d/%d audits:%d launch:%s", status, payloadState, deletedAt, version, saveCount, metadataCount, contentCount, contentFileCount, variantCount, variantFileCount, auditCount, launchState)
	afterDeleteRequest := httptest.NewRequestWithContext(ctx, http.MethodGet, launchConfiguration.GameURL, nil)
	afterDeleteRequest.Header.Set("Cache-Control", "no-cache")
	afterDeleteRequest.AddCookie(runtimeGrant)
	afterDelete := httptest.NewRecorder()
	handler.ServeHTTP(afterDelete, afterDeleteRequest)
	testassert.Falsef(t, afterDelete.Code != http.StatusUnauthorized ||
		!strings.Contains(afterDelete.Body.String(), `"code":"LAUNCH_CREDENTIAL_INVALID"`),
		"runtime content after hard delete = %d %s", afterDelete.Code, afterDelete.Body.String())
	publicList := httptest.NewRecorder()
	handler.ServeHTTP(publicList, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games?limit=100", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return publicList.Code != http.StatusOK }, func() bool { return strings.Contains(publicList.Body.String(), gameID) }), "deleted game remained public = %d %s", publicList.Code, publicList.Body.String())
	admin := httptest.NewRecorder()
	handler.ServeHTTP(admin, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/games/"+gameID, nil))
	testassert.Falsef(t, testassert.Any(func() bool { return admin.Code != http.StatusOK }, func() bool { return !strings.Contains(admin.Body.String(), `"status":"DELETED"`) }), "deleted admin history = %d %s", admin.Code, admin.Body.String())
	favoritesHistory := httptest.NewRecorder()
	favoritesRequest := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/favorites", nil)
	setCSRFCredentials(favoritesRequest, cookie, csrf)
	handler.ServeHTTP(favoritesHistory, favoritesRequest)
	testassert.Falsef(t, testassert.Any(
		func() bool { return favoritesHistory.Code != http.StatusOK },
		func() bool { return !strings.Contains(favoritesHistory.Body.String(), `"gameId":"`+gameID+`"`) },
		func() bool { return !strings.Contains(favoritesHistory.Body.String(), `"status":"DELETED"`) },
		func() bool { return !strings.Contains(favoritesHistory.Body.String(), `"coverUrl":null`) },
	), "deleted favorite tombstone = %d %s", favoritesHistory.Code, favoritesHistory.Body.String())
	recentHistory := httptest.NewRecorder()
	recentRequest := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/recent-games", nil)
	setCSRFCredentials(recentRequest, cookie, csrf)
	handler.ServeHTTP(recentHistory, recentRequest)
	testassert.Falsef(t, testassert.Any(
		func() bool { return recentHistory.Code != http.StatusOK },
		func() bool { return !strings.Contains(recentHistory.Body.String(), `"gameId":"`+gameID+`"`) },
		func() bool { return !strings.Contains(recentHistory.Body.String(), `"availability":"DELETED"`) },
		func() bool { return !strings.Contains(recentHistory.Body.String(), `"coverUrl":null`) },
	), "deleted recent tombstone = %d %s", recentHistory.Code, recentHistory.Body.String())
	var protectedBlob, prematureCandidate int64
	if err := server.database.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM blobs WHERE id=?),
(SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?)`, blobID, blobID).
		Scan(&protectedBlob, &prematureCandidate); err != nil || protectedBlob != 1 || prematureCandidate != 0 {
		t.Fatalf("shared blob after first delete = blob:%d candidate:%d error:%v", protectedBlob, prematureCandidate, err)
	}
	sharedImpact, err := payloadrelease.GameDeleteImpact(ctx, server.database, sharedGameID)
	if err != nil {
		t.Fatal(err)
	}
	sharedDeleted := sendDelete(sharedGameID, `"v1"`, "Move fixture194", sharedImpact.ImpactDigest, uuid.NewString())
	if sharedDeleted.Code != http.StatusAccepted {
		t.Fatalf("delete last shared game = %d %s", sharedDeleted.Code, sharedDeleted.Body.String())
	}
	waitForPayloadState(t, server.database, sharedGameID, "RELEASED")
	var candidateCount int64
	if err := server.database.QueryRowContext(ctx,
		`SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?`, blobID,
	).Scan(&candidateCount); err != nil || candidateCount != 1 {
		t.Fatalf("last shared release candidate = %d, error=%v", candidateCount, err)
	}
}

func waitForPayloadState(t *testing.T, database *sql.DB, gameID, expected string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.QueryRowContext(context.Background(), `SELECT payload_state FROM games WHERE id=?`, gameID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("game %s payload state = %s", gameID, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newReadyHTTPServer(t *testing.T) *Server {
	t.Helper()
	server := newTestServer(t)
	if err := server.dependencies.Bootstrap(context.Background(), server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(context.Background(), server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	return server
}

func httpSession(t *testing.T, server *Server) (http.Handler, *http.Cookie, string) {
	t.Helper()
	cookie, token := testSessionCredentials()
	return server.Handler(), cookie, token
}

func seedMovableGame(t *testing.T, server *Server) (string, string) {
	t.Helper()
	ctx := context.Background()
	var artifactID string
	if err := server.database.QueryRowContext(ctx, `
SELECT id
FROM core_artifacts
WHERE core_id='gambatte'
AND selected_for_new_bindings=1
`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	contents := []byte("move-game")
	metadata, err := server.blobs.Put(bytes.NewReader(contents))
	testassert.False(t, err != nil, err)
	blobID, err := blobstore.EnsureRecord(ctx, server.database, metadata, "application/octet-stream", time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	gameID := "01980000-0000-7000-8000-000000000176"
	metadataID := "01980000-0000-7000-8000-000000000177"
	contentID := "01980000-0000-7000-8000-000000000178"
	variantID := "01980000-0000-7000-8000-000000000179"
	revisionID := "01980000-0000-7000-8000-000000000180"
	validationDigest, dependencySnapshot := validationFixture(t, server.database, artifactID, contentID, "move.gbc")
	now := time.Now().UnixMilli()
	transaction, err := server.database.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	statements := []struct {
		query string
		args  []any
	}{
		{`PRAGMA defer_foreign_keys=ON`, nil},
		{`
INSERT INTO game_metadata_revisions(id, game_id, title, title_initial, description, developer, publisher, genre, players,
release_year, source_kind, source_ref_id, created_at_ms)
VALUES(?, ?, 'Move fixture', 'M', '', '', '', '', NULL, NULL, 'ADMIN_EDIT', NULL, ?)
`, []any{metadataID, gameID, now}},
		{`
INSERT INTO game_content_revisions(id, game_id, source_kind, source_ref_id, source_manifest_json,
source_manifest_digest, created_at_ms)
VALUES(?, ?, 'ADMIN_REPLACE', 'fixture', '{}', ?, ?)
`, []any{contentID, gameID, strings.Repeat("1", 64), now}},
		{`
INSERT INTO games(id, platform_instance_id, status, current_metadata_revision_id, current_content_revision_id,
search_text, version, created_at_ms, updated_at_ms)
VALUES(?, (SELECT id FROM platform_instances WHERE catalog_template_key='gbc/gambatte'), 'PUBLISHED', ?, ?, 'move fixture', 1, ?, ?)
`, []any{gameID, metadataID, contentID, now, now}},
		{`
INSERT INTO game_content_files(game_content_revision_id, role, logical_name, blob_id, sort_order)
VALUES(?, 'CONTENT', 'move.gbc', ?, 0)
`, []any{contentID, blobID}},
		{`
INSERT INTO game_variants(id, game_id, core_id, current_revision_id, version, created_at_ms, updated_at_ms)
VALUES(?, ?, 'gambatte', NULL, 1, ?, ?)
`, []any{variantID, gameID, now, now}},
		{`
INSERT INTO game_variant_revisions(id, game_variant_id, game_content_revision_id, core_artifact_id,
route_key, dat_version_id, validation_input_digest, emulator_game_id, status, compatibility_code,
dependency_snapshot_json, created_at_ms)
VALUES(?, ?, ?, ?, 'DEFAULT', NULL, ?, 7001, 'READY', 'READY', ?, ?)
`, []any{revisionID, variantID, contentID, artifactID, validationDigest, dependencySnapshot, now}},
		{`
UPDATE game_variants
SET current_revision_id=?
WHERE id=?
`, []any{revisionID, variantID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed movable game: %v", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return gameID, contentID
}

func cloneMovableGame(
	t *testing.T,
	server *Server,
	sourceGameID, gameSuffix, metadataSuffix, contentSuffix, variantSuffix, revisionSuffix string,
) {
	t.Helper()
	id := func(suffix string) string { return "01980000-0000-7000-8000-000000000" + suffix }
	ctx := context.Background()
	var artifactID string
	if err := server.database.QueryRowContext(ctx, `SELECT core_artifact_id FROM game_variant_revisions WHERE id=(
SELECT current_revision_id FROM game_variants WHERE game_id=? AND core_id='gambatte')`, sourceGameID).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	validationDigest, _ := validationFixture(t, server.database, artifactID, id(contentSuffix), "move.gbc")
	transaction, err := server.database.BeginTx(ctx, nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`
INSERT INTO game_metadata_revisions(id, game_id, title, title_initial, description, developer, publisher, genre, players,
release_year, source_kind, source_ref_id, created_at_ms)
SELECT ?, ?, title || ?, title_initial, description, developer, publisher, genre, players, release_year, source_kind, source_ref_id,
created_at_ms
FROM game_metadata_revisions
WHERE id=(SELECT current_metadata_revision_id FROM games WHERE id=?)
`, []any{id(metadataSuffix), id(gameSuffix), gameSuffix, sourceGameID}},
		{`
INSERT INTO game_content_revisions(id, game_id, source_kind, source_ref_id, source_manifest_json,
source_manifest_digest, created_at_ms)
SELECT ?, ?, source_kind, source_ref_id, source_manifest_json, source_manifest_digest, created_at_ms
FROM game_content_revisions
WHERE id=(SELECT current_content_revision_id FROM games WHERE id=?)
`, []any{id(contentSuffix), id(gameSuffix), sourceGameID}},
		{`
INSERT INTO games(id, platform_instance_id, status, current_metadata_revision_id, current_content_revision_id,
search_text, version, created_at_ms, updated_at_ms)
SELECT ?, platform_instance_id, status, ?, ?, search_text || ?, 1, created_at_ms, updated_at_ms
FROM games
WHERE id=?
`, []any{id(gameSuffix), id(metadataSuffix), id(contentSuffix), gameSuffix, sourceGameID}},
		{`
INSERT INTO game_content_files(game_content_revision_id, role, logical_name, blob_id, sort_order,
source_archive_blob_id, source_archive_entry_ordinal)
SELECT ?, role, logical_name, blob_id, sort_order, source_archive_blob_id, source_archive_entry_ordinal
FROM game_content_files
WHERE game_content_revision_id=(SELECT current_content_revision_id FROM games WHERE id=?)
`, []any{id(contentSuffix), sourceGameID}},
		{`
INSERT INTO game_variants(id, game_id, core_id, current_revision_id, version, created_at_ms, updated_at_ms)
SELECT ?, ?, core_id, NULL, 1, created_at_ms, updated_at_ms
FROM game_variants
WHERE game_id=? AND core_id='gambatte'
`, []any{id(variantSuffix), id(gameSuffix), sourceGameID}},
		{`
INSERT INTO game_variant_revisions(id, game_variant_id, game_content_revision_id, core_artifact_id,
route_key, dat_version_id, validation_input_digest, emulator_game_id, status, compatibility_code,
dependency_snapshot_json, created_at_ms)
SELECT ?, ?, ?, core_artifact_id, route_key, dat_version_id, ?, emulator_game_id + ?, status, compatibility_code,
dependency_snapshot_json, created_at_ms
FROM game_variant_revisions
WHERE id=(SELECT current_revision_id FROM game_variants WHERE game_id=? AND core_id='gambatte')
		`, []any{
			id(revisionSuffix), id(variantSuffix), id(contentSuffix),
			validationDigest,
			mustSuffixInt(t, gameSuffix), sourceGameID,
		}},
		{`UPDATE game_variants SET current_revision_id=? WHERE id=?`, []any{id(revisionSuffix), id(variantSuffix)}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("clone movable game: %v", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func seedProductSave(t *testing.T, database *sql.DB, saveID, launchID, name string) {
	t.Helper()
	var profileID, gameID, contentID, variantID, artifactID string
	var adapterABI, dependencyJSON, payloadKind, payloadBlobID, payloadSHA256 string
	var datVersionID, dosEntryPath sql.NullString
	var payloadSize int64
	if err := database.QueryRowContext(context.Background(), `
SELECT launch.profile_id,launch.game_id,launch.game_content_revision_id,
 launch.game_variant_revision_id,launch.core_artifact_id,
 json_extract(artifact.compatibility_json,'$.adapterAbi'),revision.dependency_snapshot_json,
 artifact.save_payload_kind,content.blob_id,blob.sha256,blob.size_bytes,
 revision.dat_version_id,launch.dos_entry_path
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
JOIN game_content_files content
  ON content.game_content_revision_id=launch.game_content_revision_id AND content.role='CONTENT'
JOIN blobs blob ON blob.id=content.blob_id
WHERE launch.id=?
ORDER BY content.sort_order,content.logical_name
LIMIT 1
`, launchID).Scan(
		&profileID, &gameID, &contentID, &variantID, &artifactID,
		&adapterABI, &dependencyJSON, &payloadKind, &payloadBlobID, &payloadSHA256, &payloadSize,
		&datVersionID, &dosEntryPath,
	); err != nil {
		t.Fatal(err)
	}
	dependencyDigest := sha256.Sum256([]byte(dependencyJSON))
	now := time.Now().UnixMilli()
	if _, err := database.ExecContext(context.Background(), `
INSERT INTO save_states(
 id,profile_id,game_id,game_content_revision_id,game_variant_revision_id,core_artifact_id,
 adapter_abi,dependency_snapshot_sha256,dat_version_id,dos_entry_path,
 payload_blob_id,payload_kind,native_profile,resume_slot,payload_sha256,payload_size_bytes,
 screenshot_blob_id,name,active_duration_ms,version,created_at_ms,updated_at_ms,
 source_launch_session_id,disc_index)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,NULL,?,?,NULL,?,0,1,?,?,?,NULL)
`, saveID, profileID, gameID, contentID, variantID, artifactID, adapterABI,
		hex.EncodeToString(dependencyDigest[:]), datVersionID, dosEntryPath,
		payloadBlobID, payloadKind, payloadSHA256, payloadSize, name, now, now, launchID); err != nil {
		t.Fatal(err)
	}
}

func validationFixture(
	t *testing.T,
	database *sql.DB,
	artifactID, contentID, logicalName string,
) (string, string) {
	t.Helper()
	snapshot, _, _, err := corevalidation.ResolveBIOS(context.Background(), database, artifactID, logicalName)
	testassert.False(t, err != nil, err)
	digest, err := corevalidation.ValidationInputDigest(artifactID, contentID, sql.NullString{}, snapshot)
	testassert.False(t, err != nil, err)
	encoded, err := snapshot.JSON()
	testassert.False(t, err != nil, err)
	return digest, string(encoded)
}

func mustSuffixInt(t *testing.T, value string) int64 {
	t.Helper()
	var result int64
	if _, err := fmt.Sscan(value, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func waitForHTTPJob(t *testing.T, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, jobID, expected string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		var errorCode sql.NullString
		if err := database.QueryRowContext(
			context.Background(), "SELECT state,error_code FROM jobs WHERE id=?", jobID,
		).Scan(&state, &errorCode); err != nil {
			t.Fatal(err)
		}
		if state == expected {
			return
		}
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return state == "CANCELLED" }, func() bool { return time.Now().After(deadline) }), "job %s state = %s error_code=%q, wanted %s", jobID, state, errorCode.String, expected)
		time.Sleep(10 * time.Millisecond)
	}
}
