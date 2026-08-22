package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
)

func TestPlatformLifecycleUsesImpactDigestVersioningAndAudit(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO core_artifacts(id,
core_id,
emulatorjs_version,
bundle_version,
flavor,
relative_path,
size_bytes,
sha256,
source_commit,
provenance_json,
compatibility_config_json,
enabled,
version,
created_at_ms,
updated_at_ms) VALUES('01980000-0000-7000-8000-000000000099',
'mgba',
'4.2.3',
'test',
'WASM',
'data/cores/mgba-test.data',
1,
?,
NULL,
'{}',
'{}',
1,
1,
0,
0)
`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	cookie, csrfToken := testSessionCredentials()
	send := func(method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		setCSRFCredentials(request, cookie, csrfToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	created := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"gambatte","name":"Handheld Zone","description":"测试目录","sortOrder":120}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	testassert.Falsef(t, created.Code != http.StatusCreated, "create platform instance = %d %s", created.Code, created.Body.String())
	var createdBody struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	invalidCore := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"fceumm","name":"错误核心","description":"","sortOrder":122}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return invalidCore.Code != http.StatusUnprocessableEntity }, func() bool {
		return !strings.Contains(invalidCore.Body.String(), `"code":"PLATFORM_DEFAULT_CORE_INVALID"`)
	}), "invalid platform core = %d %s", invalidCore.Code, invalidCore.Body.String())
	duplicateName := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"gambatte","name":"Handheld Zone","description":"","sortOrder":123}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return duplicateName.Code != http.StatusCreated }, func() bool { return !strings.Contains(duplicateName.Body.String(), `"slug":"handheld-zone-2"`) }), "generated duplicate platform slug = %d %s", duplicateName.Code, duplicateName.Body.String())
	patched := send(
		http.MethodPatch,
		"/api/v1/admin/platform-instances/"+createdBody.ID,
		`{"name":"掌机典藏","description":"测试目录","sortOrder":121,"enabled":true}`,
		map[string]string{"If-Match": `"v1"`},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return patched.Code != http.StatusOK }, func() bool { return patched.Header().Get("ETag") != `"v2"` }), "patch platform instance = %d %s", patched.Code, patched.Body.String())
	preview := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances/"+createdBody.ID+"/default-core-preview",
		`{"coreId":"mgba","cursor":null,"limit":50}`,
		map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()},
	)
	testassert.Falsef(t, preview.Code != http.StatusOK, "preview default core = %d %s", preview.Code, preview.Body.String())
	var previewBody struct {
		ImpactDigest string `json:"impactDigest"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewBody); err != nil || previewBody.ImpactDigest == "" {
		t.Fatalf("preview body = %s, error=%v", preview.Body.String(), err)
	}
	changed := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances/"+createdBody.ID+"/default-core",
		fmt.Sprintf(`{"coreId":"mgba","impactDigest":%q,"confirmBlocked":false}`, previewBody.ImpactDigest),
		map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return changed.Code != http.StatusOK }, func() bool { return changed.Header().Get("ETag") != `"v3"` }), "change default core = %d %s", changed.Code, changed.Body.String())
	stale := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances/"+createdBody.ID+"/default-core",
		fmt.Sprintf(`{"coreId":"gambatte","impactDigest":%q,"confirmBlocked":false}`, previewBody.ImpactDigest),
		map[string]string{"If-Match": `"v2"`, "Idempotency-Key": uuid.NewString()},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return stale.Code != http.StatusConflict }, func() bool { return !strings.Contains(stale.Body.String(), `"code":"IMPACT_PREVIEW_STALE"`) }), "stale impact = %d %s", stale.Code, stale.Body.String())
	deleted := send(
		http.MethodDelete,
		"/api/v1/admin/platform-instances/"+createdBody.ID,
		"",
		map[string]string{"If-Match": `"v3"`},
	)
	testassert.Falsef(t, deleted.Code != http.StatusNoContent, "delete platform instance = %d %s", deleted.Code, deleted.Body.String())
	reusedSlug := send(
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		`{"platformId":"gbc","defaultCoreId":"gambatte","name":"Handheld Zone","description":"","sortOrder":124}`,
		map[string]string{"Idempotency-Key": uuid.NewString()},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return reusedSlug.Code != http.StatusCreated }, func() bool { return !strings.Contains(reusedSlug.Body.String(), `"slug":"handheld-zone-3"`) }), "deleted platform slug was reused = %d %s", reusedSlug.Code, reusedSlug.Body.String())
	var actions, distinctActors int
	var actorKind string
	if err := server.database.QueryRowContext(context.Background(), `
SELECT count(*),count(DISTINCT actor_user_id),min(actor_kind)
FROM audit_events
WHERE resource_type='PLATFORM_INSTANCE'
AND resource_id=?
`, createdBody.ID).Scan(&actions, &distinctActors, &actorKind); err != nil ||
		actions != 4 || distinctActors != 1 || actorKind != "USER" {
		t.Fatalf("platform audit actions = %d actors=%d/%s, error=%v", actions, distinctActors, actorKind, err)
	}
}

func TestPlatformInstanceOrderIsAtomicVersionedAndExact(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	handler := server.Handler()
	create := func(name string, sortOrder int) string {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(fmt.Sprintf(
			`{"platformId":"gbc","defaultCoreId":"gambatte","name":%q,"description":"","sortOrder":%d}`,
			name, sortOrder,
		)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", uuid.NewString())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		testassert.Falsef(t, recorder.Code != http.StatusCreated, "create reorder fixture = %d %s", recorder.Code, recorder.Body.String())
		var body struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.ID
	}
	firstID := create("第一目录", 100)
	secondID := create("第二目录", 200)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/platform-instances", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return list.Code != http.StatusOK }, func() bool { return strings.Count(list.Body.String(), `"gameCount":0`) != 29 }), "platform game counts = %d %s", list.Code, list.Body.String())
	sendOrder := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/admin/platform-instances/order", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	type orderItem struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	items := []orderItem{{ID: secondID, Version: 1}, {ID: firstID, Version: 1}}
	func() {
		rows, err := server.database.QueryContext(context.Background(),
			"SELECT id,version FROM platform_instances WHERE deleted_at_ms IS NULL ORDER BY sort_order,id",
		)
		testassert.False(t, err != nil, err)
		defer func() { cleanup.Error("close", rows.Close()) }()
		for rows.Next() {
			var item orderItem
			if err := rows.Scan(&item.ID, &item.Version); err != nil {
				t.Fatal(err)
			}
			if item.ID != firstID && item.ID != secondID {
				items = append(items, item)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}()
	orderBody, err := json.Marshal(map[string]any{"items": items})
	testassert.False(t, err != nil, err)
	reordered := sendOrder(string(orderBody))
	testassert.Falsef(t, testassert.Any(func() bool { return reordered.Code != http.StatusOK }, func() bool { return !strings.Contains(reordered.Body.String(), `"sortOrder":100`) }, func() bool { return !strings.Contains(reordered.Body.String(), `"version":2`) }), "platform reorder = %d %s", reordered.Code, reordered.Body.String())
	var firstSort, firstVersion, secondSort, secondVersion int64
	if err := server.database.QueryRowContext(context.Background(), "SELECT sort_order,version FROM platform_instances WHERE id=?", firstID).Scan(&firstSort, &firstVersion); err != nil {
		t.Fatal(err)
	}
	if err := server.database.QueryRowContext(context.Background(), "SELECT sort_order,version FROM platform_instances WHERE id=?", secondID).Scan(&secondSort, &secondVersion); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return secondSort != 100 }, func() bool { return firstSort != 200 }, func() bool { return firstVersion != 2 }, func() bool { return secondVersion != 2 }), "stored reorder first=%d/v%d second=%d/v%d", firstSort, firstVersion, secondSort, secondVersion)
	stale := sendOrder(string(orderBody))
	testassert.Falsef(t, testassert.Any(func() bool { return stale.Code != http.StatusConflict }, func() bool { return !strings.Contains(stale.Body.String(), `"code":"VERSION_CONFLICT"`) }), "stale reorder = %d %s", stale.Code, stale.Body.String())
	incomplete := sendOrder(fmt.Sprintf(`{"items":[{"id":%q,"version":2}]}`, firstID))
	testassert.Falsef(t, testassert.Any(func() bool { return incomplete.Code != http.StatusConflict }, func() bool {
		return !strings.Contains(incomplete.Body.String(), `"code":"PLATFORM_INSTANCE_ORDER_STALE"`)
	}), "incomplete reorder = %d %s", incomplete.Code, incomplete.Body.String())
}
