package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestHealthIsPublicAndProtectedWritesRequireAuthentication(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()

	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health/live", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return live.Code != http.StatusOK }, func() bool { return live.Header().Get("X-Request-ID") == "" }), "live status = %d, request id = %q", live.Code, live.Header().Get("X-Request-ID"))

	requestBody := `{"platformId":"gbc","defaultCoreId":"gambatte","name":"Protected","description":"","sortOrder":900}`
	unauthenticated := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost,
		"/api/v1/admin/platform-instances",
		strings.NewReader(requestBody),
	)
	unauthenticated.Header.Set("Content-Type", "application/json")
	unauthenticated.Header.Set("Idempotency-Key", uuid.NewString())
	unauthenticated.Header.Set("Origin", "http://localhost:3000")
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, unauthenticated)
	testassert.Falsef(t, testassert.Any(func() bool { return denied.Code != http.StatusUnauthorized }, func() bool { return !strings.Contains(denied.Body.String(), "AUTHENTICATION_REQUIRED") }), "anonymous write = %d %s", denied.Code, denied.Body.String())

	auth := accountHTTPLogin(t, handler)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("X-Retrom-Csrf", auth.csrf)
	request.AddCookie(auth.cookie)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, request)
	testassert.Falsef(t, created.Code != http.StatusCreated, "authenticated write status = %d %s", created.Code, created.Body.String())
}

func TestAuthenticationMiddlewareClearsCookieOnlyForDefinitiveRevocation(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	cookie, _ := testSessionCredentials()

	server.authenticator = fixedAuthenticator{Err: errors.New("database unavailable")}
	unavailableRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games", nil)
	unavailableRequest.AddCookie(cookie)
	unavailable := httptest.NewRecorder()
	server.Handler().ServeHTTP(unavailable, unavailableRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return unavailable.Code != http.StatusInternalServerError }, func() bool { return unavailable.Header().Values("Set-Cookie") != nil }), "temporary auth failure = %d cookies=%v body=%s", unavailable.Code, unavailable.Header().Values("Set-Cookie"), unavailable.Body.String())

	server.authenticator = fixedAuthenticator{Err: accounts.ErrAuthenticationNeeded}
	revokedRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/games", nil)
	revokedRequest.AddCookie(cookie)
	revoked := httptest.NewRecorder()
	server.Handler().ServeHTTP(revoked, revokedRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return revoked.Code != http.StatusUnauthorized }, func() bool { return len(revoked.Header().Values("Set-Cookie")) == 0 }), "revoked auth = %d cookies=%v body=%s", revoked.Code, revoked.Header().Values("Set-Cookie"), revoked.Body.String())
}

func TestProtectedWritesRejectInvalidOriginWithoutEnablingCORS(t *testing.T) {
	t.Parallel()
	server, _ := newAuthHTTPServer(t, config.ModeTest)
	handler := server.Handler()
	auth := accountHTTPLogin(t, handler)
	send := func(name string, headers map[string]string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(
			`{"platformId":"gbc","defaultCoreId":"gambatte","name":%q,"description":"","sortOrder":900}`,
			"LAN "+name,
		)
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/platform-instances", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", uuid.NewString())
		request.Header.Set("X-Retrom-Csrf", auth.csrf)
		request.AddCookie(auth.cookie)
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		testassert.Falsef(t, recorder.Header().Get("Access-Control-Allow-Origin") != "", "CORS header unexpectedly present: %q", recorder.Header().Get("Access-Control-Allow-Origin"))
		return recorder
	}
	if response := send("no-origin", nil); response.Code != http.StatusForbidden {
		t.Fatalf("request without origin = %d %s", response.Code, response.Body.String())
	}
	crossOrigin := send("cross-origin", map[string]string{
		"Origin":         "https://external.example",
		"Sec-Fetch-Site": "cross-site",
	})
	testassert.Falsef(t, crossOrigin.Code != http.StatusForbidden, "cross-origin write = %d %s", crossOrigin.Code, crossOrigin.Body.String())
	sameOrigin := send("same-origin", map[string]string{
		"Origin":         "http://localhost:3000",
		"Sec-Fetch-Site": "same-origin",
	})
	testassert.Falsef(t, sameOrigin.Code != http.StatusCreated, "same-origin write = %d %s", sameOrigin.Code, sameOrigin.Body.String())
}

func TestIdempotencyRecordsAreScopedToAuthenticatedUser(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	var calls int
	handler := server.idempotencyHandler(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		principal, _ := authn.PrincipalFromContext(request.Context())
		writeJSON(writer, http.StatusCreated, map[string]string{"userId": principal.UserID})
	}))
	key := uuid.NewString()
	send := func(userID string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/test-principal-scope", strings.NewReader(`{"value":1}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		ctx := context.WithValue(request.Context(), operationIDContextKey, "PostPrincipalScopeFixture")
		ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: userID, ProfileID: userID + "-profile"})
		request = request.WithContext(ctx)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	const userA = "01980000-0000-7000-8000-000000009991"
	const userB = "01980000-0000-7000-8000-000000009992"
	firstA := send(userA)
	firstB := send(userB)
	replayA := send(userA)
	testassert.Falsef(t, testassert.Any(func() bool { return firstA.Code != http.StatusCreated }, func() bool { return firstB.Code != http.StatusCreated }, func() bool { return replayA.Code != http.StatusCreated }, func() bool { return !strings.Contains(firstA.Body.String(), userA) }, func() bool { return !strings.Contains(firstB.Body.String(), userB) }, func() bool { return replayA.Header().Get("X-Retrom-Idempotent-Replay") != "true" }, func() bool { return calls != 2 }), "principal idempotency responses: A=%d %s B=%d %s replay=%d %s calls=%d", firstA.Code, firstA.Body.String(), firstB.Code, firstB.Body.String(), replayA.Code, replayA.Body.String(), calls)
	var records int
	if err := server.database.QueryRowContext(context.Background(),
		`SELECT count(*) FROM idempotency_records WHERE operation_id='postPrincipalScopeFixture' AND key=?`,
		key,
	).Scan(&records); err != nil || records != 2 {
		t.Fatalf("principal idempotency records = %d, error=%v", records, err)
	}
}

func TestRuntimeAllowlistRejectsUnknownPath(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	recorder := httptest.NewRecorder()
	server.Handler().
		ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/runtime/emulatorjs/4.2.3/not-in-manifest.js", nil))
	testassert.Falsef(t, recorder.Code != http.StatusNotFound, "unknown runtime path status = %d", recorder.Code)
}

func TestLaunchBundleHEADUsesCredentialProtectedHandler(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	identity := strings.Repeat("a", 64)
	for _, path := range []string{
		"/runtime/content/bios/" + identity + "/bundle.zip",
		"/runtime/content/parent/" + identity + "/bundle.zip",
	} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodHead, path, nil))
		testassert.Falsef(t, testassert.Any(func() bool { return recorder.Code != http.StatusUnauthorized }, func() bool { return !strings.Contains(recorder.Body.String(), `"code":"LAUNCH_CREDENTIAL_INVALID"`) }), "HEAD %s = %d: %s", path, recorder.Code, recorder.Body.String())
	}
}

func TestBIOSArchiveEntriesProjectLockedDATAndPersistedZIPFacts(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	const (
		artifactID     = "01980000-0000-7000-8000-000000000201"
		datVersionID   = "01980000-0000-7000-8000-000000000202"
		requirementID  = "01980000-0000-7000-8000-000000000203"
		blobID         = "01980000-0000-7000-8000-000000000204"
		installationID = "01980000-0000-7000-8000-000000000205"
	)
	const now = int64(1_786_269_147_906)
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	seedHTTPTestCoreArtifact(t, transaction, artifactID, "mame2003_plus", "data/cores/mame2003_plus-test.data", strings.Repeat("a", 64), "{}")
	target, err := testsupport.LookupRuntimeTarget(t.Context(), transaction, "mame2003_plus")
	testassert.False(t, err != nil, err)
	mustExecHTTPTest(t, transaction, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,1024,?,?,?,'application/zip',?)
`, blobID, strings.Repeat("b", 64), strings.Repeat("c", 32), strings.Repeat("d", 40), strings.Repeat("e", 8), now)
	mustExecHTTPTest(t, transaction, `
INSERT INTO dat_versions(id,core_id,provider_id,target_id,target_contract_sha256,builtin_relative_path,sha256,parser_version,
parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,bios_set_count,
default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,unresolved_relation_count,
version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES(?,'mame2003_plus',?,?,?,'test.dat',?,'test','READY',1,1,1,0,0,0,1,0,0,1,?,?,?,?)
`, datVersionID, target.ProviderID, target.TargetID, target.TargetContractSHA256,
		strings.Repeat("f", 64), now, now, now, now)
	mustExecHTTPTest(t, transaction, `
INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,is_explicit_bios,classification)
VALUES(?,'stvbios','ST-V BIOS','','SEGA',1,'EXPLICIT_BIOS')
`, datVersionID)
	mustExecHTTPTest(t, transaction, `
INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status,bios_name)
VALUES(?,'stvbios',0,'epr19730.ic8',524288,'d0e0889d',?,'GOOD',NULL)
`, datVersionID, strings.Repeat("1", 40))
	mustExecHTTPTest(t, transaction, `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,target_contract_sha256,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,catalog_digest,source_url,source_version,enabled,version,created_at_ms,updated_at_ms)
VALUES(?,'mame2003_plus',?,?,?,'DAT_MACHINE','stvbios','stvbios.zip','REQUIRED','ARCADE_DAT_DEPENDENCY',?,
'retrom:test',?,1,1,?,?)
`, requirementID, target.ProviderID, target.TargetID, target.TargetContractSHA256,
		strings.Repeat("2", 64), datVersionID, now, now)
	mustExecHTTPTest(t, transaction, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'stvbios.zip',1024,?,?,?,1,'MATCHED','{}',1,1,?,?)
`, installationID, requirementID, blobID, strings.Repeat("c", 32), strings.Repeat("d", 40), strings.Repeat("b", 64), now, now)
	mustExecHTTPTest(t, transaction, `
INSERT INTO archive_entries(archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,materialized_blob_id,created_at_ms)
VALUES(?,0,'epr-19730.ic8','epr-19730.ic8','epr-19730.ic8','ZIP','STORE',524288,'d0e0889d',?,?,?,NULL,?)
`, blobID, strings.Repeat("3", 32), strings.Repeat("1", 40), strings.Repeat("4", 64), now)
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	server.Handler().ServeHTTP(list, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/bios?scope=FULL_CATALOG", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return list.Code != http.StatusOK }, func() bool { return !strings.Contains(list.Body.String(), `"sourceKind":"DAT_MACHINE"`) }), "BIOS list = %d %s", list.Code, list.Body.String())
	entries := httptest.NewRecorder()
	server.Handler().ServeHTTP(entries, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet,
		"/api/v1/admin/bios/"+requirementID+"/entries",
		nil,
	))
	testassert.Falsef(t, testassert.Any(func() bool { return entries.Code != http.StatusOK }, func() bool { return !strings.Contains(entries.Body.String(), `"status":"ALIASED"`) }, func() bool { return !strings.Contains(entries.Body.String(), `"name":"epr19730.ic8"`) }, func() bool { return !strings.Contains(entries.Body.String(), `"name":"epr-19730.ic8"`) }), "BIOS entries = %d %s", entries.Code, entries.Body.String())
}

func TestRestrictedBinaryEndpointsRejectMultipleRanges(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/runtime/content/game/"+strings.Repeat("a", 64)+"/game.zip", nil)
	request.Header.Set("Range", "bytes=0-1,4-5")
	recorder := httptest.NewRecorder()
	testassert.Falsef(t, testassert.Any(func() bool { return !rejectMultipleRanges(recorder, request) }, func() bool { return recorder.Code != http.StatusRequestedRangeNotSatisfiable }, func() bool { return recorder.Header().Get("Content-Range") != "" }, func() bool { return !strings.Contains(recorder.Body.String(), `"code":"MULTIPLE_RANGES_UNSUPPORTED"`) }), "multiple range response = %d %s", recorder.Code, recorder.Body.String())
}

func TestDiagnosticsUsesClosedSnapshotSchemaAndRequiredHeaders(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	fixed := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return fixed }
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/diagnostics", nil))
	testassert.Falsef(t, testassert.Any(func() bool { return recorder.Code != http.StatusOK }, func() bool { return recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" }, func() bool { return recorder.Header().Get("Cache-Control") != "private, no-store" }, func() bool {
		return recorder.Header().Get("Content-Disposition") != `attachment; filename="retrom-diagnostics.json"`
	}, func() bool { return recorder.Header().Get("X-Content-Type-Options") != "nosniff" }), "diagnostics response = %d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	var response struct {
		SchemaVersion         int64 `json:"schemaVersion"`
		GeneratedAtMS         int64 `json:"generatedAtMs"`
		DatabaseSchemaVersion int64 `json:"databaseSchemaVersion"`
		RuntimeProviders      []struct {
			ProviderID      string `json:"providerId"`
			ProviderVersion string `json:"providerVersion"`
			BundleSHA256    string `json:"bundleSha256"`
			Source          string `json:"source"`
		} `json:"runtimeProviders"`
		Counts struct {
			Games struct {
				Published int64 `json:"published"`
				Deleted   int64 `json:"deleted"`
			} `json:"games"`
			SaveStates struct {
				Active  int64 `json:"active"`
				Deleted int64 `json:"deleted"`
			} `json:"saveStates"`
			Blobs int64 `json:"blobs"`
			Jobs  struct {
				Queued          int64 `json:"queued"`
				Running         int64 `json:"running"`
				CancelRequested int64 `json:"cancelRequested"`
				Succeeded       int64 `json:"succeeded"`
				Failed          int64 `json:"failed"`
				Cancelled       int64 `json:"cancelled"`
			} `json:"jobs"`
			DATVersions struct {
				Pending   int64 `json:"pending"`
				Parsing   int64 `json:"parsing"`
				Ready     int64 `json:"ready"`
				Failed    int64 `json:"failed"`
				Cancelled int64 `json:"cancelled"`
			} `json:"datVersions"`
		} `json:"counts"`
	}
	decoder := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("diagnostics schema: %v: %s", err, recorder.Body.String())
	}
	testassert.Falsef(t, testassert.Any(func() bool { return response.SchemaVersion != 2 }, func() bool { return response.GeneratedAtMS != fixed.UnixMilli() }, func() bool { return response.DatabaseSchemaVersion != 10 }, func() bool { return len(response.RuntimeProviders) != 2 }, func() bool { return response.RuntimeProviders[0].ProviderID != "emulatorjs" }, func() bool { return response.RuntimeProviders[1].ProviderID != "retrom-runtime" }), "diagnostics values = %#v", response)
}

func TestImportProjectionsIncludeRejectedFileProblems(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	now := time.Now()
	if err := server.dependencies.Bootstrap(context.Background(), server.database, now); err != nil {
		t.Fatal(err)
	}
	target, err := testsupport.LookupRuntimeTarget(t.Context(), server.database, "fceumm")
	if err != nil {
		t.Fatal(err)
	}
	const (
		importID     = "01980000-0000-7000-8000-000000000140"
		uploadID     = "01980000-0000-7000-8000-000000000141"
		uploadFileID = "01980000-0000-7000-8000-000000000142"
		blobID       = "01980000-0000-7000-8000-000000000143"
	)
	digest := strings.Repeat("f", 64)
	timestamp := now.UnixMilli()
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	mustExecHTTPTest(t, transaction, `PRAGMA defer_foreign_keys=ON`)
	mustExecHTTPTest(t, transaction, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,1,?,?,?,'application/zip',?)
`, blobID, digest, strings.Repeat("a", 32), strings.Repeat("b", 40), strings.Repeat("c", 8), timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,1,?,1,?,?,?)
`, uploadID, digest, timestamp+60_000, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?,'fc/8只眼.zip',1,1,?,'COMPLETE',?,?)
`, uploadFileID, uploadID, blobID, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
provider_id,target_id,target_contract_sha256,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,rejected_file_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,(SELECT id FROM platform_instances WHERE catalog_template_key='nes/fceumm'),1,'nes','fceumm',?,?,?,'HASHEOUS','{}',?,'PARTIAL_FAILURE',0,1,1,?,?)
`, importID, uploadID, target.ProviderID, target.TargetID, target.TargetContractSHA256, digest, timestamp, timestamp)
	mustExecHTTPTest(t, transaction, `
INSERT INTO import_job_files(import_job_id,upload_file_id,disposition,reason_code,created_at_ms,updated_at_ms)
VALUES(?,?,'REJECTED','ARCHIVE_UNSAFE',?,?)
`, importID, uploadFileID, timestamp, timestamp)
	for index := 0; index < 20; index++ {
		extraUploadID := fmt.Sprintf("01980000-0000-7000-8000-%012d", 200+index)
		extraImportID := fmt.Sprintf("01980000-0000-7000-8001-%012d", 200+index)
		extraTimestamp := timestamp - int64(index+1)
		mustExecHTTPTest(t, transaction, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,0,?,1,?,?,?)
`, extraUploadID, digest, timestamp+60_000, extraTimestamp, extraTimestamp)
		mustExecHTTPTest(t, transaction, `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
provider_id,target_id,target_contract_sha256,metadata_provider,config_snapshot_json,config_snapshot_digest,state,version,created_at_ms,updated_at_ms)
VALUES(?,?,(SELECT id FROM platform_instances WHERE catalog_template_key='nes/fceumm'),1,'nes','fceumm',?,?,?,'HASHEOUS','{}',?,'COMPLETED',1,?,?)
`, extraImportID, extraUploadID, target.ProviderID, target.TargetID,
			target.TargetContractSHA256, digest, extraTimestamp, extraTimestamp)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	summaryResponse := httptest.NewRecorder()
	server.importSummary(
		summaryResponse,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/imports/summary", nil),
	)
	var overview struct {
		Running         int64 `json:"running"`
		ReviewPending   int64 `json:"reviewPending"`
		PublishedItems  int64 `json:"publishedItems"`
		Completed       int64 `json:"completed"`
		Failed          int64 `json:"failed"`
		OrdinaryFailed  int64 `json:"ordinaryFailed"`
		PegasusFailed   int64 `json:"pegasusFailed"`
		ProcessingItems int64 `json:"processingItems"`
		IssueItems      int64 `json:"issueItems"`
	}
	decodeErr := json.Unmarshal(summaryResponse.Body.Bytes(), &overview)
	testassert.Falsef(t, anyTrue(decodeErr != nil, summaryResponse.Code != http.StatusOK,
		overview.Running != 0, overview.ReviewPending != 0, overview.PublishedItems != 0,
		overview.Completed != 20, overview.Failed != 1, overview.OrdinaryFailed != 1,
		overview.PegasusFailed != 0, overview.ProcessingItems != 0, overview.IssueItems != 1),
		"import overview = %d %s, parsed=%#v error=%v",
		summaryResponse.Code, summaryResponse.Body.String(), overview, decodeErr)
	list := httptest.NewRecorder()
	server.imports(list, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/imports?limit=20", nil))
	testassert.Falsef(t, anyTrue(list.Code != http.StatusOK,
		!strings.Contains(list.Body.String(), `"rejectedFileCount":1`),
		!strings.Contains(list.Body.String(), `"contentMode":"STANDARD"`)),
		"import list = %d %s", list.Code, list.Body.String())
	var firstPage struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	decodeErr = json.Unmarshal(list.Body.Bytes(), &firstPage)
	testassert.Falsef(t, anyTrue(decodeErr != nil, len(firstPage.Items) != 20, firstPage.NextCursor == nil),
		"first import page = %#v, error=%v", firstPage, decodeErr)
	testassert.Falsef(t, firstPage.Items[0].ID != importID, "first import page = %#v", firstPage)
	second := httptest.NewRecorder()
	server.imports(second, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet,
		"/api/v1/admin/imports?limit=20&cursor="+url.QueryEscape(*firstPage.NextCursor),
		nil,
	))
	var secondPage struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	decodeErr = json.Unmarshal(second.Body.Bytes(), &secondPage)
	testassert.Falsef(t, anyTrue(decodeErr != nil, second.Code != http.StatusOK,
		len(secondPage.Items) != 1, secondPage.NextCursor != nil),
		"second import page = %d %#v, error=%v", second.Code, secondPage, decodeErr)
	overLimit := httptest.NewRecorder()
	server.imports(overLimit, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/imports?limit=21", nil))
	testassert.Falsef(t, overLimit.Code != http.StatusBadRequest, "over-limit import page = %d %s", overLimit.Code, overLimit.Body.String())
	detail := httptest.NewRecorder()
	detailRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/imports/"+importID, nil)
	detailRequest.SetPathValue("importJobId", importID)
	server.importDetail(detail, detailRequest)
	testassert.Falsef(t, anyTrue(detail.Code != http.StatusOK,
		!strings.Contains(detail.Body.String(), `"disposition":"REJECTED"`),
		!strings.Contains(detail.Body.String(), `"name":"fc/8只眼.zip"`),
		!strings.Contains(detail.Body.String(), `"reasonCode":"ARCHIVE_UNSAFE"`),
		!strings.Contains(detail.Body.String(), `"unresolvedRejectedFiles":1`)),
		"import detail = %d %s", detail.Code, detail.Body.String())
}

func TestImportOverviewCountsPegasusOnceAndHidesItsInternalJob(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	now := time.Now()
	if err := server.dependencies.Bootstrap(context.Background(), server.database, now); err != nil {
		t.Fatal(err)
	}
	target, err := testsupport.LookupRuntimeTarget(t.Context(), server.database, "mgba")
	if err != nil {
		t.Fatal(err)
	}
	const (
		importID       = "01980000-0000-7000-8000-000000000150"
		uploadID       = "01980000-0000-7000-8000-000000000151"
		itemID         = "01980000-0000-7000-8000-000000000152"
		pegasusID      = "01980000-0000-7000-8000-000000000153"
		pegasusItemID  = "01980000-0000-7000-8000-000000000154"
		pegasusScanJob = "01980000-0000-7000-8000-000000000155"
	)
	digest := strings.Repeat("a", 64)
	timestamp := now.UnixMilli()
	mustExecHTTPTest(t, server.database, `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,1,?,1,?,?,?)
`, uploadID, digest, timestamp+60_000, timestamp, timestamp)
	mustExecHTTPTest(t, server.database, `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
platform_id,default_core_id,provider_id,target_id,target_contract_sha256,metadata_provider,config_snapshot_json,config_snapshot_digest,
state,total_item_count,review_pending_item_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),1,'gba','mgba',?,?,?,'NONE','{}',?,
'REVIEW_PENDING',1,1,1,?,?)
`, importID, uploadID, target.ProviderID, target.TargetID, target.TargetContractSHA256, digest, timestamp, timestamp)
	mustExecHTTPTest(t, server.database, `
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,version,created_at_ms,updated_at_ms,completed_at_ms)
VALUES(?,?,?,'DISCARDED','{"files":[]}',?,'discarded.gba',2,?,?,?)
`, itemID, importID, digest, digest, timestamp, timestamp, timestamp)
	var userID string
	if err := server.database.QueryRowContext(context.Background(), `SELECT id FROM users WHERE status='ENABLED' ORDER BY id LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	mustExecHTTPTest(t, server.database, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,1,?,?,?,?)
`, pegasusScanJob, pegasusID, strings.Repeat("b", 64), timestamp, timestamp, timestamp, timestamp)
	mustExecHTTPTest(t, server.database, `
INSERT INTO pegasus_imports(
 id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,scan_job_id,
 game_count,processable_item_count,review_discarded_item_count,created_by_user_id,
 created_at_ms,updated_at_ms,completed_at_ms,expires_at_ms
) VALUES(?,'games','Games','FBNeo',?,'COMPLETED',?,1,1,1,?,?,?,?,?)
`, pegasusID, strings.Repeat("c", 64), pegasusScanJob, userID,
		timestamp, timestamp, timestamp, timestamp+60_000)
	mustExecHTTPTest(t, server.database, `
INSERT INTO pegasus_import_items(
 id,import_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,execution_state,
 metadata_json,source_manifest_json,source_manifest_digest,retryable,
 library_import_job_id,library_import_item_id,created_at_ms,updated_at_ms,completed_at_ms
) VALUES(?,?,'FBNeo/metadata.pegasus.txt',0,?,'Fixture','READY','REVIEW_DISCARDED',
 '{}','{"files":[]}',?,0,?,?,?,?,?)
	`, pegasusItemID, pegasusID, strings.Repeat("d", 64), digest,
		importID, itemID, timestamp, timestamp, timestamp)
	recorder := httptest.NewRecorder()
	server.importSummary(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/imports/summary", nil))
	var summary struct {
		Running         int64 `json:"running"`
		ReviewPending   int64 `json:"reviewPending"`
		PublishedItems  int64 `json:"publishedItems"`
		Completed       int64 `json:"completed"`
		Failed          int64 `json:"failed"`
		OrdinaryFailed  int64 `json:"ordinaryFailed"`
		PegasusFailed   int64 `json:"pegasusFailed"`
		ProcessingItems int64 `json:"processingItems"`
		IssueItems      int64 `json:"issueItems"`
	}
	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &summary)
	testassert.Falsef(t, anyTrue(decodeErr != nil, recorder.Code != http.StatusOK,
		summary.Running != 0, summary.ReviewPending != 0, summary.PublishedItems != 0,
		summary.Completed != 1, summary.Failed != 0, summary.OrdinaryFailed != 0,
		summary.PegasusFailed != 0, summary.ProcessingItems != 0, summary.IssueItems != 0),
		"import summary = %d %s, parsed=%#v error=%v", recorder.Code, recorder.Body.String(), summary, decodeErr)
	list := httptest.NewRecorder()
	server.imports(list, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/imports?limit=20", nil))
	var page struct {
		Items []importListItem `json:"items"`
	}
	decodeErr = json.Unmarshal(list.Body.Bytes(), &page)
	testassert.Falsef(t, anyTrue(decodeErr != nil, list.Code != http.StatusOK, len(page.Items) != 0),
		"user-visible import list = %d %s, parsed=%#v error=%v", list.Code, list.Body.String(), page, decodeErr)
}

func TestReviewWorkflowQueriesAreAllowed(t *testing.T) {
	t.Parallel()
	requests := []*http.Request{
		httptest.NewRequestWithContext(context.Background(),
			http.MethodGet,
			"/api/v1/admin/reviews?pegasusImportId=01980000-0000-7000-8000-000000000001&limit=20",
			nil,
		),
		httptest.NewRequestWithContext(context.Background(),
			http.MethodGet,
			"/api/v1/admin/reviews?emulationStationImportId=01980000-0000-7000-8000-000000000005&limit=20",
			nil,
		),
		httptest.NewRequestWithContext(context.Background(),
			http.MethodGet,
			"/api/v1/admin/review-assets/01980000-0000-7000-8000-000000000002?kind=VIDEO",
			nil,
		),
		httptest.NewRequestWithContext(context.Background(),
			http.MethodHead,
			"/api/v1/admin/review-assets/01980000-0000-7000-8000-000000000002?kind=VIDEO",
			nil,
		),
		httptest.NewRequestWithContext(context.Background(),
			http.MethodGet,
			"/api/v1/admin/review-bulk-approval-preview?importJobId=01980000-0000-7000-8000-000000000003&blockerCode=MISSING_BIOS",
			nil,
		),
		httptest.NewRequestWithContext(context.Background(),
			http.MethodGet,
			"/api/v1/admin/review-bulk-approval-preview?emulationStationImportId=01980000-0000-7000-8000-000000000006",
			nil,
		),
		httptest.NewRequestWithContext(context.Background(),
			http.MethodGet,
			"/api/v1/admin/review-bulk-approvals/01980000-0000-7000-8000-000000000004/items?outcome=PUBLISHED&cursor=10&limit=50",
			nil,
		),
	}
	for _, request := range requests {
		if err := validateQueryValues(request.URL.Query(), queryAllowlist(request)); err != nil {
			t.Fatalf("review workflow query %s %s rejected: %v", request.Method, request.URL, err)
		}
	}
}
