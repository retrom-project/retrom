package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/rpgmaker/packs"
	"retrom/internal/uploads"
)

func TestRuntimeAssetPackRoutesListAndMapProductErrors(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()

	listRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/admin/runtime-asset-packs", nil,
	)
	listRequest.AddCookie(cookie)
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK {
		t.Fatalf("GET runtime packs = %d %s", list.Code, list.Body.String())
	}
	var catalog packs.ListView
	if err := json.Unmarshal(list.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Definitions) != 5 || catalog.Definitions[0].NormalizedDeclaredName == "" ||
		catalog.Installations == nil {
		t.Fatalf("runtime pack catalog = %#v", catalog)
	}

	installRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/v1/admin/runtime-asset-packs/installations",
		strings.NewReader(`{"uploadId":"01980000-0000-7000-8000-000000009997","kind":"RPG2000_RTP"}`),
	)
	installRequest.Header.Set("Content-Type", "application/json")
	installRequest.Header.Set("Idempotency-Key", uuid.NewString())
	installRequest.Header.Set("Origin", "http://localhost:3000")
	installRequest.Header.Set("X-Retrom-Csrf", csrf)
	installRequest.AddCookie(cookie)
	install := httptest.NewRecorder()
	handler.ServeHTTP(install, installRequest)
	if install.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(install.Body.String(), `"code":"RPG_RUNTIME_PACK_INVALID"`) {
		t.Fatalf("POST missing upload = %d %s", install.Code, install.Body.String())
	}
	unavailable := httptest.NewRecorder()
	server.writeRuntimeAssetPackError(
		unavailable, installRequest, fmt.Errorf("archive worker: %w", packs.ErrUnavailable),
	)
	if unavailable.Code != http.StatusServiceUnavailable ||
		!strings.Contains(unavailable.Body.String(), `"code":"RPG_RUNTIME_PACK_UNAVAILABLE"`) {
		t.Fatalf("POST unavailable worker = %d %s", unavailable.Code, unavailable.Body.String())
	}

	deleteRequest := httptest.NewRequestWithContext(
		context.Background(), http.MethodDelete,
		"/api/v1/admin/runtime-asset-packs/installations/01980000-0000-7000-8000-000000009996", nil,
	)
	deleteRequest.Header.Set("If-Match", `"v1"`)
	deleteRequest.Header.Set("Idempotency-Key", uuid.NewString())
	deleteRequest.Header.Set("Origin", "http://localhost:3000")
	deleteRequest.Header.Set("X-Retrom-Csrf", csrf)
	deleteRequest.AddCookie(cookie)
	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusNotFound ||
		!strings.Contains(deleted.Body.String(), `"code":"RPG_RUNTIME_PACK_NOT_FOUND"`) {
		t.Fatalf("DELETE missing installation = %d %s", deleted.Code, deleted.Body.String())
	}
}

func TestRuntimeAssetPackInstallAndDeleteRoutesCompleteProductTransaction(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()
	cookie, csrf := testSessionCredentials()
	uploadID := completeRuntimePackUpload(t, server)

	install := runtimePackWriteRequest(
		t, handler, cookie, csrf, http.MethodPost,
		"/api/v1/admin/runtime-asset-packs/installations", uuid.NewString(), "",
		fmt.Sprintf(`{"uploadId":%q,"kind":"RPG2000_RTP"}`, uploadID),
	)
	if install.Code != http.StatusAccepted {
		t.Fatalf("POST runtime pack = %d %s", install.Code, install.Body.String())
	}
	var accepted packs.InstallAccepted
	if err := json.Unmarshal(install.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	waitRuntimePackHTTPJob(t, server, accepted.JobID, "SUCCEEDED")

	var version int64
	if err := server.database.QueryRowContext(t.Context(), `
SELECT version FROM runtime_asset_pack_installations WHERE id=? AND status='READY'
`, accepted.InstallationID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	deleted := runtimePackWriteRequest(
		t, handler, cookie, csrf, http.MethodDelete,
		"/api/v1/admin/runtime-asset-packs/installations/"+accepted.InstallationID,
		key, fmt.Sprintf(`"v%d"`, version), "",
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("DELETE runtime pack = %d %s", deleted.Code, deleted.Body.String())
	}
	replay := runtimePackWriteRequest(
		t, handler, cookie, csrf, http.MethodDelete,
		"/api/v1/admin/runtime-asset-packs/installations/"+accepted.InstallationID,
		key, fmt.Sprintf(`"v%d"`, version), "",
	)
	if replay.Code != http.StatusNoContent || replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" {
		t.Fatalf("DELETE runtime pack replay = %d headers=%v", replay.Code, replay.Header())
	}
	var consumptions int
	if err := server.database.QueryRowContext(t.Context(), `
SELECT count(*) FROM upload_consumptions
WHERE upload_session_id=? AND consumer_type='RUNTIME_ASSET_PACK_INSTALLATION'
`, uploadID).Scan(&consumptions); err != nil || consumptions != 1 {
		t.Fatalf("runtime pack upload consumptions = %d, error=%v", consumptions, err)
	}
}

func completeRuntimePackUpload(t *testing.T, server *Server) string {
	t.Helper()
	contents := []byte("\x89PNG\r\n\x1a\nretrom-http-pack")
	session, err := server.uploads.Create(t.Context(), uploads.CreateRequest{
		Purpose: "RUNTIME_ASSET_PACK", SourceType: "DIRECTORY",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "backdrop", RelativePath: "RTP/Backdrop/Dungeon1.png",
			SizeBytes: int64(len(contents)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	err = server.uploads.PutPart(
		t.Context(), session.ID, session.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := server.uploads.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := server.uploads.Complete(t.Context(), session.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitRuntimePackHTTPJob(t, server, jobID, "SUCCEEDED")
	return session.ID
}

func waitRuntimePackHTTPJob(t *testing.T, server *Server, jobID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err := server.database.QueryRowContext(
			t.Context(), "SELECT state FROM jobs WHERE id=?", jobID,
		).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == want {
			return
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("job %s state = %s, want %s", jobID, state, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runtimePackWriteRequest(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	csrf, method, target, idempotencyKey, ifMatch, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("X-Retrom-Csrf", csrf)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if ifMatch != "" {
		request.Header.Set("If-Match", ifMatch)
	}
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestReviewRuntimePackRequirementsExposeEasyAndRGSSSlots(t *testing.T) {
	var easy reviewRPGAnalysisProjection
	got := reviewRPGPackRequirements("RPG2000", easy)
	if len(got) != 1 || got[0].Slot != 0 || got[0].DeclaredName != "RPG2000_RTP" ||
		got[0].NormalizedDeclaredName != "rpg2000_rtp" {
		t.Fatalf("EasyRPG requirements = %#v", got)
	}
	var rgss reviewRPGAnalysisProjection
	rgss.Requirements.RTP = []reviewRPGPackRequirement{{Slot: 2, DeclaredName: "Custom"}}
	got = reviewRPGPackRequirements("RPGVX", rgss)
	if len(got) != 1 || got[0].Slot != 2 || got[0].DeclaredName != "Custom" ||
		got[0].NormalizedDeclaredName != "custom" {
		t.Fatalf("RGSS requirements = %#v", got)
	}
	rgss.SelfContained = true
	if got = reviewRPGPackRequirements("RPGVX", rgss); len(got) != 0 || got == nil {
		t.Fatalf("self-contained requirements = %#v", got)
	}
}
