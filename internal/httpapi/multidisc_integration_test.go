//go:build integration

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

	"retrom/internal/blobstore"
	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

type multiDiscHTTPFile struct {
	path     string
	contents []byte
}

func completeMultiDiscHTTPUpload(
	t *testing.T,
	server *Server,
	sourceType string,
	files []multiDiscHTTPFile,
) string {
	t.Helper()
	declarations := make([]uploads.FileDeclaration, 0, len(files))
	for index, file := range files {
		declarations = append(declarations, uploads.FileDeclaration{
			ClientFileID: fmt.Sprintf("disc-%d", index), RelativePath: file.path,
			SizeBytes: int64(len(file.contents)),
		})
	}
	ctx := context.Background()
	upload, err := server.uploads.Create(ctx, uploads.CreateRequest{SourceType: sourceType, Files: declarations})
	testassert.False(t, err != nil, err)
	for index, file := range files {
		digest := sha256.Sum256(file.contents)
		if err := server.uploads.PutPart(
			ctx, upload.ID, upload.Files[index].ID, 0,
			fmt.Sprintf("bytes 0-%d/%d", len(file.contents)-1, len(file.contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
			bytes.NewReader(file.contents),
		); err != nil {
			t.Fatal(err)
		}
	}
	current, err := server.uploads.Get(ctx, upload.ID)
	testassert.False(t, err != nil, err)
	jobID, _, err := server.uploads.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitForHTTPJob(t, server.database, jobID, "SUCCEEDED")
	return upload.ID
}

func seedMultiDiscHTTPBIOS(t *testing.T, server *Server) {
	t.Helper()
	ctx := context.Background()
	metadata, err := server.blobs.Put(bytes.NewReader([]byte("deterministic HTTP Saturn BIOS fixture")))
	testassert.False(t, err != nil, err)
	blobID, err := blobstore.EnsureRecord(ctx, server.database, metadata, "application/octet-stream", time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	var requirementID string
	var requirementVersion int64
	if err := server.database.QueryRowContext(ctx, `
SELECT id,version FROM bios_requirements
WHERE core_id='yabause' AND logical_name='saturn_bios.bin' AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	installationID := uuid.NewString()
	if _, err := server.database.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',1,1,?,?)
`, installationID, requirementID, blobID, "saturn_bios.bin", metadata.Size, metadata.MD5, metadata.SHA1,
		metadata.SHA256, requirementVersion, time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func multiDiscHTTPCHD(value string) []byte {
	return append([]byte("MComprHD"), []byte(value)...)
}

func createMultiDiscHTTPLaunch(t *testing.T, server *Server) (launch.Created, string) {
	t.Helper()
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	server.importer.WithMultiDiscImportEnabled(true)
	seedMultiDiscHTTPBIOS(t, server)
	uploadID := completeMultiDiscHTTPUpload(t, server, "DIRECTORY", []multiDiscHTTPFile{
		{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\n")},
		{path: "game/one.chd", contents: multiDiscHTTPCHD("one")},
		{path: "game/two.chd", contents: multiDiscHTTPCHD("two")},
	})
	createdImport, err := server.importer.Create(ctx, libraryimport.CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, server.database, "saturn/yabause"),
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	testassert.False(t, err != nil, err)
	var itemID string
	if err := server.database.QueryRowContext(
		ctx, `SELECT id FROM import_items WHERE import_job_id=?`, createdImport.ImportJobID,
	).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	approved, err := server.importer.Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
	created, err := server.launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: launch.Capabilities{
			SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
		},
	})
	testassert.False(t, err != nil, err)
	return created, approved.GameID
}

func addParentBundleToLaunch(t *testing.T, server *Server, created launch.Created) {
	t.Helper()
	metadata, err := server.blobs.Put(bytes.NewReader([]byte("deterministic parent bundle fixture")))
	testassert.False(t, err != nil, err)
	blobID, err := blobstore.EnsureRecord(
		t.Context(), server.database, metadata, "application/zip", time.Now().UnixMilli(),
	)
	testassert.False(t, err != nil, err)
	var revisionID string
	if err := server.database.QueryRowContext(t.Context(),
		`SELECT game_variant_revision_id FROM launch_sessions WHERE id=?`, created.LaunchID,
	).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(t.Context(), `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'PARENT','parent.zip',?,0)
`, revisionID, blobID); err != nil {
		t.Fatal(err)
	}
}

type runtimeContentRequester func(string, string, func(*http.Request)) *httptest.ResponseRecorder

func assertImmutableRuntimeGETAndHEAD(
	t *testing.T,
	contentURL string,
	requestContent runtimeContentRequester,
) string {
	t.Helper()
	get := requestContent(http.MethodGet, contentURL, nil)
	etag := get.Header().Get("ETag")
	testassert.Falsef(t, testassert.Any(
		func() bool { return get.Code != http.StatusOK },
		func() bool { return etag == "" },
		func() bool { return get.Header().Get("Cache-Control") != immutablePrivateContent },
	), "runtime GET %s = %d headers=%v", contentURL, get.Code, get.Header())
	head := requestContent(http.MethodHead, contentURL, nil)
	testassert.Falsef(t, testassert.Any(
		func() bool { return head.Code != http.StatusOK },
		func() bool { return head.Body.Len() != 0 },
		func() bool { return head.Header().Get("ETag") != etag },
		func() bool { return head.Header().Get("Content-Length") != get.Header().Get("Content-Length") },
	), "runtime HEAD %s = %d headers=%v", contentURL, head.Code, head.Header())
	revalidated := requestContent(http.MethodGet, contentURL, func(request *http.Request) {
		request.Header.Set("If-None-Match", etag)
	})
	testassert.Falsef(t, revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0,
		"runtime revalidation %s = %d body=%q", contentURL, revalidated.Code, revalidated.Body.String())
	return etag
}

func TestRuntimeContentIsPrivateImmutableRevalidatesAndRevokes(t *testing.T) {
	server := newTestServer(t)
	created, gameID := createMultiDiscHTTPLaunch(t, server)
	addParentBundleToLaunch(t, server, created)
	handler := server.Handler()
	configRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/runtime/launches/"+created.LaunchID+"/config", nil)
	configRequest.AddCookie(&http.Cookie{
		Name: "retrom_launch_" + created.LaunchID, Value: created.Capability,
		Path: "/runtime/launches/" + created.LaunchID + "/",
	})
	configResponse := httptest.NewRecorder()
	handler.ServeHTTP(configResponse, configRequest)
	var configuration launch.Config
	mustDecodeHTTPTest(t, configResponse.Body.Bytes(), &configuration)
	var grant *http.Cookie
	for _, candidate := range configResponse.Result().Cookies() {
		if candidate.Name == runtimeContentGrantPrefix+created.LaunchID {
			grant = candidate
			break
		}
	}
	testassert.Falsef(t, configResponse.Code != http.StatusOK || grant == nil,
		"launch config = %d headers=%v body=%s", configResponse.Code, configResponse.Header(), configResponse.Body.String())
	requestContent := func(method, contentURL string, configure func(*http.Request)) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), method, contentURL, nil)
		if grant != nil {
			request.AddCookie(grant)
		}
		if configure != nil {
			configure(request)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	withoutGrant := httptest.NewRecorder()
	handler.ServeHTTP(withoutGrant, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, configuration.GameURL, nil,
	))
	testassert.Falsef(t, withoutGrant.Code != http.StatusUnauthorized,
		"runtime content without grant = %d %s", withoutGrant.Code, withoutGrant.Body.String())

	game := requestContent(http.MethodGet, configuration.GameURL, nil)
	gameETag := game.Header().Get("ETag")
	testassert.Falsef(t, testassert.Any(
		func() bool { return game.Code != http.StatusOK },
		func() bool { return gameETag == "" },
		func() bool { return game.Header().Get("Cache-Control") != immutablePrivateContent },
		func() bool { return game.Header().Get("Vary") != "" },
	), "runtime game = %d headers=%v body=%q", game.Code, game.Header(), game.Body.String())
	revalidated := requestContent(http.MethodGet, configuration.GameURL, func(request *http.Request) {
		request.Header.Set("If-None-Match", gameETag)
	})
	testassert.Falsef(t, revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0,
		"runtime revalidation = %d headers=%v body=%q", revalidated.Code, revalidated.Header(), revalidated.Body.String())

	firstDiscURL := configuration.ExternalFiles["/disc-001.chd"]
	discRange := requestContent(http.MethodGet, firstDiscURL, func(request *http.Request) {
		request.Header.Set("Range", "bytes=0-3")
	})
	testassert.Falsef(t, testassert.Any(
		func() bool { return discRange.Code != http.StatusPartialContent },
		func() bool { return discRange.Body.String() != "MCom" },
		func() bool { return discRange.Header().Get("Cache-Control") != immutablePrivateContent },
	), "disc range = %d headers=%v body=%q", discRange.Code, discRange.Header(), discRange.Body.String())
	discHead := requestContent(http.MethodHead, firstDiscURL, nil)
	testassert.Falsef(t, discHead.Code != http.StatusOK || discHead.Body.Len() != 0,
		"disc HEAD = %d headers=%v body=%q", discHead.Code, discHead.Header(), discHead.Body.String())
	biosURL, biosOK := configuration.BIOSURL.(string)
	parentURL, parentOK := configuration.ParentURL.(string)
	testassert.Falsef(t, !biosOK || !parentOK, "dependency URLs = BIOS:%#v parent:%#v", configuration.BIOSURL, configuration.ParentURL)
	if biosOK {
		assertImmutableRuntimeGETAndHEAD(t, biosURL, requestContent)
	}
	if parentOK {
		assertImmutableRuntimeGETAndHEAD(t, parentURL, requestContent)
	}

	second, err := server.launcher.Create(t.Context(), "local", launch.CreateRequest{
		GameID: gameID, ReturnTo: "/games/" + gameID,
		ClientCapabilities: launch.Capabilities{
			SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
		},
	})
	testassert.False(t, err != nil, err)
	secondConfiguration, err := server.launcher.Config(t.Context(), second.LaunchID, second.Capability)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return secondConfiguration.GameURL != configuration.GameURL },
		func() bool { return secondConfiguration.BIOSURL != configuration.BIOSURL },
		func() bool { return secondConfiguration.ParentURL != configuration.ParentURL },
		func() bool { return secondConfiguration.ExternalFiles["/disc-001.chd"] != firstDiscURL },
	), "cross-launch URLs = first:%#v second:%#v error=%v", configuration, secondConfiguration, err)

	if _, err := server.database.ExecContext(t.Context(),
		`UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1 WHERE id=?`,
		time.Now().UnixMilli(), time.Now().UnixMilli(), created.LaunchID,
	); err != nil {
		t.Fatal(err)
	}
	revoked := requestContent(http.MethodGet, configuration.GameURL, func(request *http.Request) {
		request.Header.Set("Cache-Control", "no-cache")
	})
	testassert.Falsef(t, revoked.Code != http.StatusUnauthorized ||
		!strings.Contains(revoked.Body.String(), `"code":"LAUNCH_CREDENTIAL_INVALID"`),
		"revoked content = %d headers=%v body=%s", revoked.Code, revoked.Header(), revoked.Body.String())
}

func TestMultiDiscAttachmentHTTPContractAndReviewProjection(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	server.importer.WithMultiDiscImportEnabled(true)
	seedMultiDiscHTTPBIOS(t, server)
	baseUploadID := completeMultiDiscHTTPUpload(t, server, "DIRECTORY", []multiDiscHTTPFile{
		{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\nthree.chd\n")},
		{path: "game/one.chd", contents: multiDiscHTTPCHD("one")},
		{path: "game/two.chd", contents: multiDiscHTTPCHD("two")},
		{path: "game/notes.txt", contents: []byte("not referenced")},
	})
	createdImport, err := server.importer.Create(ctx, libraryimport.CreateRequest{
		UploadID: baseUploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, server.database, "saturn/yabause"),
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	testassert.False(t, err != nil, err)
	var itemID string
	if err := server.database.QueryRowContext(
		ctx, `SELECT id FROM import_items WHERE import_job_id=?`, createdImport.ImportJobID,
	).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	importDetailRequest := httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/v1/admin/imports/"+createdImport.ImportJobID, nil,
	)
	importDetailRequest.SetPathValue("importJobId", createdImport.ImportJobID)
	importDetail := httptest.NewRecorder()
	server.importDetail(importDetail, importDetailRequest)
	for _, expected := range []string{
		`"contentMode":"MULTI_DISC_M3U_V1"`,
		`"itemSummaries":[`,
		`"contentKind":"MULTI_DISC_M3U_V1"`,
		`"playlist":"game.m3u"`,
		`"discCount":3`,
		`"presentDiscCount":2`,
		`"missingDiscCount":1`,
		`"ignoredFileCount":1`,
		`"ignoredFiles":["notes.txt"]`,
	} {
		testassert.Falsef(t, testassert.Any(func() bool { return importDetail.Code != http.StatusOK }, func() bool { return !strings.Contains(importDetail.Body.String(), expected) }), "import detail missing %s = %d %s", expected, importDetail.Code, importDetail.Body.String())
	}
	testassert.Falsef(t, strings.Contains(importDetail.Body.String(), `"blobId"`), "import detail exposes blob id = %s", importDetail.Body.String())
	attachmentUploadID := completeMultiDiscHTTPUpload(t, server, "FILES", []multiDiscHTTPFile{
		{path: "three.chd", contents: multiDiscHTTPCHD("three")},
	})
	handler, cookie, csrf := httpSession(t, server)
	key := uuid.NewString()
	send := func() *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(),
			http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/multi-disc-attachments",
			strings.NewReader(fmt.Sprintf(`{"uploadId":%q}`, attachmentUploadID)),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"v1"`)
		request.Header.Set("Idempotency-Key", key)
		setCSRFCredentials(request, cookie, csrf)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := send()
	var attachment struct {
		AttachmentID  string `json:"attachmentId"`
		JobID         string `json:"jobId"`
		State         string `json:"state"`
		ReviewVersion int64  `json:"reviewVersion"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &attachment); err != nil || first.Code != http.StatusAccepted ||
		attachment.AttachmentID == "" || attachment.JobID == "" || attachment.State != "QUEUED" ||
		attachment.ReviewVersion != 2 || first.Header().Get("ETag") != `"v2"` ||
		first.Header().Get("Location") != "/api/v1/admin/jobs/"+attachment.JobID {
		t.Fatalf("attachment create = %d %s, headers=%v, error=%v", first.Code, first.Body.String(), first.Header(), err)
	}
	replay := send()
	testassert.Falsef(t, testassert.Any(func() bool { return replay.Code != http.StatusAccepted }, func() bool { return replay.Body.String() != first.Body.String() }, func() bool { return replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" }, func() bool { return replay.Header().Get("ETag") != `"v2"` }), "attachment replay = %d %s, headers=%v", replay.Code, replay.Body.String(), replay.Header())
	waitForHTTPJob(t, server.database, attachment.JobID, "SUCCEEDED")
	reviewRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	reviewRequest.AddCookie(cookie)
	review := httptest.NewRecorder()
	handler.ServeHTTP(review, reviewRequest)
	var reviewProjection struct {
		CanApprove bool            `json:"canApprove"`
		MultiDisc  json.RawMessage `json:"multiDisc"`
	}
	if err := json.Unmarshal(review.Body.Bytes(), &reviewProjection); err != nil || review.Code != http.StatusOK ||
		!reviewProjection.CanApprove || !bytes.Contains(reviewProjection.MultiDisc, []byte(`"missingDiscCount":0`)) ||
		!bytes.Contains(reviewProjection.MultiDisc, []byte(`"maxDiscs":8`)) ||
		!bytes.Contains(reviewProjection.MultiDisc, []byte(`"maxTotalBytes":1073741824`)) ||
		bytes.Contains(reviewProjection.MultiDisc, []byte(`"blobId"`)) {
		t.Fatalf("accepted review = %d %s", review.Code, review.Body.String())
	}
	var previousArtifactID string
	if err := server.database.QueryRowContext(ctx, `
SELECT id FROM core_artifacts
WHERE core_id='yabause' AND selected_for_new_bindings=1
`).Scan(&previousArtifactID); err != nil {
		t.Fatal(err)
	}
	transaction, err := server.database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET selected_for_new_bindings=0,version=version+1,updated_at_ms=? WHERE id=?
`, time.Now().UnixMilli(), previousArtifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO core_artifacts(
 id,core_id,route_key,runtime_family,runtime_adapter_kind,runtime_version,adapter_id,entry_path,
 size_bytes,sha256,manifest_sha256,artifact_set_sha256,requires_threads,save_payload_kind,
 save_max_bytes,provenance_json,compatibility_json,selected_for_new_bindings,available_for_launch,
 version,created_at_ms,updated_at_ms)
SELECT ?,core_id,route_key,runtime_family,runtime_adapter_kind,runtime_version,adapter_id,entry_path,
 size_bytes,sha256,manifest_sha256,?,requires_threads,save_payload_kind,
 save_max_bytes,provenance_json,json_set(compatibility_json,'$.multiDisc.maxDiscs',7),1,1,
 1,?,?
FROM core_artifacts WHERE id=?
`, uuid.NewString(), strings.Repeat("d", 64), time.Now().UnixMilli(), time.Now().UnixMilli(),
		previousArtifactID); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	staleRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	staleRequest.AddCookie(cookie)
	stale := httptest.NewRecorder()
	handler.ServeHTTP(stale, staleRequest)
	testassert.Falsef(t, testassert.Any(func() bool { return stale.Code != http.StatusOK }, func() bool { return !strings.Contains(stale.Body.String(), `"validationStale":true`) }, func() bool { return !strings.Contains(stale.Body.String(), `"canApprove":false`) }), "compatibility-stale review = %d %s", stale.Code, stale.Body.String())
}

func TestMultiDiscPlayerEventHTTPContract(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	server.importer.WithMultiDiscImportEnabled(true)
	seedMultiDiscHTTPBIOS(t, server)
	uploadID := completeMultiDiscHTTPUpload(t, server, "DIRECTORY", []multiDiscHTTPFile{
		{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\n")},
		{path: "game/one.chd", contents: multiDiscHTTPCHD("one")},
		{path: "game/two.chd", contents: multiDiscHTTPCHD("two")},
	})
	createdImport, err := server.importer.Create(ctx, libraryimport.CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, server.database, "saturn/yabause"),
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	testassert.False(t, err != nil, err)
	var itemID string
	if err := server.database.QueryRowContext(
		ctx, `SELECT id FROM import_items WHERE import_job_id=?`, createdImport.ImportJobID,
	).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	approved, err := server.importer.Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
	createdLaunch, err := server.launcher.Create(ctx, "local", launch.CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID,
		ClientCapabilities: launch.Capabilities{
			SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
		},
	})
	testassert.False(t, err != nil, err)
	if _, err := server.launcher.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability); err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	launchCookie := &http.Cookie{
		Name: "retrom_launch_" + createdLaunch.LaunchID, Value: createdLaunch.Capability,
		Path: "/runtime/launches/" + createdLaunch.LaunchID + "/",
	}
	send := func(body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(context.Background(),
			http.MethodPost, "/runtime/launches/"+createdLaunch.LaunchID+"/player-events", strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://localhost:3000")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	accepted := send(
		`{"eventType":"START","resultCode":"OK","discCount":2,"observedDiscCount":2}`,
		launchCookie,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return accepted.Code != http.StatusNoContent }, func() bool { return accepted.Body.Len() != 0 }), "accepted event = %d %s", accepted.Code, accepted.Body.String())
	mismatched := send(
		`{"eventType":"START","resultCode":"OK","discCount":3,"observedDiscCount":3}`,
		launchCookie,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return mismatched.Code != http.StatusUnprocessableEntity }, func() bool { return !strings.Contains(mismatched.Body.String(), `"code":"PLAYER_EVENT_INVALID"`) }), "mismatched event = %d %s", mismatched.Code, mismatched.Body.String())
	unauthorized := send(
		`{"eventType":"START","resultCode":"OK","discCount":2,"observedDiscCount":2}`,
		nil,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return unauthorized.Code != http.StatusUnauthorized }, func() bool {
		return !strings.Contains(unauthorized.Body.String(), `"code":"LAUNCH_CREDENTIAL_INVALID"`)
	}), "unauthorized event = %d %s", unauthorized.Code, unauthorized.Body.String())
}
