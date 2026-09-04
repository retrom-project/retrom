package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/contentcapability"
	"retrom/internal/libraryimport"
	"retrom/internal/netplay"
	"retrom/internal/platformcatalog"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestAdminPlatformsProjectsManifestBoundNetplayCapability(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	if err := server.dependencies.Bootstrap(t.Context(), server.database, time.UnixMilli(1_786_000_000_000)); err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	testassert.False(t, err != nil, err)
	registry, err := netplay.LoadRegistry(filepath.Join(repositoryRoot, "data"), server.dependencies)
	testassert.False(t, err != nil, err)
	credentials, err := netplay.LoadOrCreateCredentials(server.config.DataDir)
	testassert.False(t, err != nil, err)
	server.WithNetplay(netplay.NewService(server.database, registry, credentials, netplay.Options{}, time.Now))

	response := httptest.NewRecorder()
	server.platforms(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/platforms", nil))
	testassert.Falsef(t, response.Code != http.StatusOK, "platform response = %d %s", response.Code, response.Body.String())
	var body struct {
		Items []struct {
			ID    string `json:"id"`
			Cores []struct {
				ID               string `json:"id"`
				NetplaySupported bool   `json:"netplaySupported"`
			} `json:"cores"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	support := make(map[string]bool)
	for _, platform := range body.Items {
		seen := make(map[string]struct{}, len(platform.Cores))
		for _, core := range platform.Cores {
			if _, exists := seen[core.ID]; exists {
				t.Fatalf("platform %s projected duplicate core %s", platform.ID, core.ID)
			}
			seen[core.ID] = struct{}{}
			support[platform.ID+"/"+core.ID] = core.NetplaySupported
		}
	}
	for _, key := range []string{
		"nes/fceumm", "nes/nestopia", "snes/snes9x", "arcade/fbneo", "arcade/mame2003",
		"arcade/mame2003_plus", "arcade/fbalpha2012_cps1", "arcade/fbalpha2012_cps2",
	} {
		testassert.Truef(t, support[key], "%s should be marked netplay capable", key)
	}
	testassert.False(t, support["gba/mgba"], "mGBA should not be marked netplay capable")
}

func TestRecommendedPlatformDirectoryHTTPApplyIsAtomicAndIdempotent(t *testing.T) {
	t.Parallel()
	server := newRecommendationTestServer(t)
	handler := server.Handler()

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/platform-instances/recommendations", nil))
	var initial struct {
		CatalogVersion int `json:"catalogVersion"`
		Summary        struct {
			TotalCount   int `json:"totalCount"`
			MissingCount int `json:"missingCount"`
		} `json:"summary"`
		Items []struct {
			TemplateKey         string   `json:"templateKey"`
			SupportedExtensions []string `json:"supportedExtensions"`
			State               string   `json:"state"`
		} `json:"items"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &initial); err != nil || get.Code != http.StatusOK {
		t.Fatalf("initial recommendations = %d %s error=%v", get.Code, get.Body.String(), err)
	}
	expectedTemplates := len(platformcatalog.Current().Templates)
	testassert.Falsef(t, testassert.Any(
		func() bool { return initial.CatalogVersion != platformcatalog.Version },
		func() bool { return initial.Summary.TotalCount != expectedTemplates },
		func() bool { return initial.Summary.MissingCount != expectedTemplates },
		func() bool { return len(initial.Items) != expectedTemplates },
	), "initial recommendations = %#v", initial)
	for _, item := range initial.Items {
		testassert.Falsef(t, testassert.Any(func() bool { return item.TemplateKey == "fds/fceumm" }, func() bool { return item.TemplateKey == "arcade/mame2003" }), "retired template was recommended: %#v", item)
		testassert.Falsef(t, testassert.All(func() bool { return item.TemplateKey == "nes/fceumm" }, func() bool { return !strings.Contains(strings.Join(item.SupportedExtensions, ","), ".fds") }), "NES extensions do not include FDS: %#v", item.SupportedExtensions)
	}

	key := uuid.NewString()
	apply := func(body string, idempotencyKey string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequestWithContext(context.Background(),
			http.MethodPost, "/api/v1/admin/platform-instances/recommendations/apply", strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", idempotencyKey)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	created := apply(`{}`, key)
	testassert.Falsef(t, testassert.Any(
		func() bool { return created.Code != http.StatusOK },
		func() bool {
			return !strings.Contains(created.Body.String(), fmt.Sprintf(`"createdCount":%d`, expectedTemplates))
		},
		func() bool { return !strings.Contains(created.Body.String(), `"remainingMissingCount":0`) },
	), "apply = %d %s", created.Code, created.Body.String())
	replayed := apply(`{}`, key)
	testassert.Falsef(t, testassert.Any(func() bool { return replayed.Code != created.Code }, func() bool { return replayed.Body.String() != created.Body.String() }, func() bool { return replayed.Header().Get("X-Retrom-Idempotent-Replay") != "true" }), "replay = %d header=%q %s", replayed.Code, replayed.Header().Get("X-Retrom-Idempotent-Replay"), replayed.Body.String())
	second := apply(`{}`, uuid.NewString())
	testassert.Falsef(t, testassert.Any(func() bool { return second.Code != http.StatusOK }, func() bool { return !strings.Contains(second.Body.String(), `"createdCount":0`) }), "second apply = %d %s", second.Code, second.Body.String())
	invalid := apply(`{"unexpected":true}`, uuid.NewString())
	testassert.Falsef(t, testassert.Any(func() bool { return invalid.Code != http.StatusBadRequest }, func() bool { return !strings.Contains(invalid.Body.String(), `"code":"INVALID_REQUEST"`) }), "invalid apply = %d %s", invalid.Code, invalid.Body.String())

	var directoryCount, auditCount int
	if err := server.database.QueryRowContext(context.Background(), `
SELECT
  (SELECT count(*) FROM platform_instances WHERE deleted_at_ms IS NULL),
  (SELECT count(*) FROM audit_events WHERE action='PLATFORM_INSTANCE_RECOMMENDED_CREATED')
`).Scan(&directoryCount, &auditCount); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(
		func() bool { return directoryCount != expectedTemplates },
		func() bool { return auditCount != expectedTemplates },
	), "applied rows = directories:%d audits:%d", directoryCount, auditCount)
}

func TestPlatformSlugBaseUsesReadableASCIIOrPlatformFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		platformID string
		want       string
	}{
		{name: "My GBA Games", platformID: "gba", want: "my-gba-games"},
		{name: "街机格斗游戏", platformID: "arcade", want: "arcade-library"},
		{name: "GBA 游戏", platformID: "gba", want: "gba"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := platformSlugBase(test.name, test.platformID); got != test.want {
				t.Fatalf("platformSlugBase(%q, %q) = %q, want %q", test.name, test.platformID, got, test.want)
			}
		})
	}
}

func decodeCreatedImport(t *testing.T, response *httptest.ResponseRecorder) libraryimport.Created {
	t.Helper()
	var created libraryimport.Created
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created
}

func waitForImportState(
	t *testing.T,
	server *Server,
	importID string,
	done func(string) bool,
) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state, code string
		if err := server.database.QueryRowContext(t.Context(), `
SELECT state,coalesce(last_error_code,'') FROM import_jobs WHERE id=?
`, importID).Scan(&state, &code); err != nil {
			t.Fatal(err)
		}
		if done(state) {
			return code
		}
		if time.Now().After(deadline) {
			t.Fatalf("import %s state = %s", importID, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCreateImportQueuesContentInspectionAndMapsImmediateAdmissionErrors(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	now := time.UnixMilli(1_786_000_000_000)
	if err := server.dependencies.Bootstrap(context.Background(), server.database, now); err != nil {
		t.Fatal(err)
	}
	server.importer.WithMultiDiscImportEnabled(true)
	metadata, err := server.blobs.Put(strings.NewReader("MComprHDdeterministic CHD fixture"))
	testassert.False(t, err != nil, err)
	blobID, err := blobstore.EnsureRecord(t.Context(), server.database, metadata, "application/octet-stream", now.UnixMilli())
	testassert.False(t, err != nil, err)
	createUpload := func(uploadID, fileID string) {
		t.Helper()
		if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','DIRECTORY',1,?, ?,1,?,?,?)
`, uploadID, metadata.Size, strings.Repeat("a", 64), now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		if _, err := server.database.ExecContext(context.Background(), `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?,'game.chd',?,?,?,'COMPLETE',?,?)
`, fileID, uploadID, metadata.Size, metadata.Size, blobID, now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	const firstUpload = "01980000-0000-7000-8000-000000007101"
	const secondUpload = "01980000-0000-7000-8000-000000007102"
	const thirdUpload = "01980000-0000-7000-8000-000000007103"
	const fourthUpload = "01980000-0000-7000-8000-000000007104"
	createUpload(firstUpload, "01980000-0000-7000-8000-000000007111")
	createUpload(secondUpload, "01980000-0000-7000-8000-000000007112")
	createUpload(thirdUpload, "01980000-0000-7000-8000-000000007113")
	createUpload(fourthUpload, "01980000-0000-7000-8000-000000007114")
	saturnID, err := testsupport.PlatformInstanceID(t.Context(), server.database, "saturn/yabause")
	testassert.False(t, err != nil, err)
	playstationID, err := testsupport.PlatformInstanceID(t.Context(), server.database, "psx/pcsx_rearmed")
	testassert.False(t, err != nil, err)
	send := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/imports", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.createImport(response, request)
		return response
	}
	missing := send(`{"uploadId":"` + firstUpload + `","targetPlatformInstanceId":"` + saturnID + `","metadataProvider":"NONE","tagIds":[],"contentMode":"MULTI_DISC"}`)
	testassert.Falsef(t, missing.Code != http.StatusAccepted, "missing playlist = %d %s", missing.Code, missing.Body.String())
	missingCreated := decodeCreatedImport(t, missing)
	missingCode := waitForImportState(t, server, missingCreated.ImportJobID, func(state string) bool {
		return state == "FAILED"
	})
	testassert.Falsef(
		t, missingCode != "MULTI_DISC_PLAYLIST_MISSING", "missing playlist code = %s", missingCode,
	)
	unsupported := send(`{"uploadId":"` + secondUpload + `","targetPlatformInstanceId":"` + playstationID + `","metadataProvider":"NONE","tagIds":[],"contentMode":"MULTI_DISC"}`)
	testassert.Falsef(t, testassert.Any(func() bool { return unsupported.Code != http.StatusUnprocessableEntity }, func() bool { return !strings.Contains(unsupported.Body.String(), "MULTI_DISC_MODE_UNAVAILABLE") }), "unsupported target = %d %s", unsupported.Code, unsupported.Body.String())
	omitted := send(`{"uploadId":"` + thirdUpload + `","targetPlatformInstanceId":"` + saturnID + `","metadataProvider":"NONE","tagIds":[]}`)
	explicit := send(`{"uploadId":"` + fourthUpload + `","targetPlatformInstanceId":"` + saturnID + `","metadataProvider":"NONE","tagIds":[],"contentMode":"STANDARD"}`)
	testassert.Falsef(t, testassert.Any(func() bool { return omitted.Code != http.StatusAccepted }, func() bool { return explicit.Code != http.StatusAccepted }), "standard admission omitted=%d %s explicit=%d %s", omitted.Code, omitted.Body.String(), explicit.Code, explicit.Body.String())
	for _, response := range []*httptest.ResponseRecorder{omitted, explicit} {
		created := decodeCreatedImport(t, response)
		waitForImportState(t, server, created.ImportJobID, func(state string) bool {
			return state != "QUEUED" && state != "RUNNING"
		})
	}
	var omittedConfig, explicitConfig, omittedDigest, explicitDigest string
	if err := server.database.QueryRowContext(context.Background(), `SELECT config_snapshot_json,config_snapshot_digest FROM import_jobs WHERE upload_session_id=?`, thirdUpload).
		Scan(&omittedConfig, &omittedDigest); err != nil {
		t.Fatal(err)
	}
	if err := server.database.QueryRowContext(context.Background(), `SELECT config_snapshot_json,config_snapshot_digest FROM import_jobs WHERE upload_session_id=?`, fourthUpload).
		Scan(&explicitConfig, &explicitDigest); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return omittedConfig != explicitConfig }, func() bool { return omittedDigest != explicitDigest }, func() bool { return !strings.Contains(omittedConfig, `"contentMode":"STANDARD"`) }), "default/explicit snapshots differ: %s/%s digest=%s/%s", omittedConfig, explicitConfig, omittedDigest, explicitDigest)
}

func TestPlatformSlugSuffixStaysWithinStorageLimit(t *testing.T) {
	t.Parallel()
	base := strings.Repeat("a", 80)
	if got := platformSlugWithSuffix(base, 12); len(got) != 80 || !strings.HasSuffix(got, "-12") {
		t.Fatalf("suffixed slug = %q (%d bytes)", got, len(got))
	}
}

type platformCapabilityProjection struct {
	PlatformID          string                               `json:"platformId"`
	Name                string                               `json:"name"`
	SupportedExtensions []string                             `json:"supportedExtensions"`
	ImportCapabilities  contentcapability.ImportCapabilities `json:"importCapabilities"`
}

func assertUniquePlatformExtensions(t *testing.T, items map[string]platformCapabilityProjection) {
	t.Helper()
	for platformID, item := range items {
		testassert.Falsef(t, len(item.SupportedExtensions) == 0, "%s has no supported extensions", platformID)
		seen := make(map[string]struct{}, len(item.SupportedExtensions))
		for _, extension := range item.SupportedExtensions {
			if _, duplicate := seen[extension]; duplicate {
				t.Fatalf("%s has duplicate extension %q", platformID, extension)
			}
			seen[extension] = struct{}{}
		}
	}
}

func assertPlatformExtensions(
	t *testing.T,
	items map[string]platformCapabilityProjection,
	wantExtensions map[string][]string,
) {
	t.Helper()
	for platformID, want := range wantExtensions {
		got := items[platformID].SupportedExtensions
		testassert.Falsef(t, len(got) != len(want), "%s extensions = %#v, want %#v", platformID, got, want)
		for index := range want {
			testassert.Falsef(t, got[index] != want[index], "%s extensions = %#v, want %#v", platformID, got, want)
		}
	}
}

func TestPlatformImportCapabilitiesUseFeaturePlatformAndArtifactIntersection(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	if err := server.dependencies.Bootstrap(t.Context(), server.database, time.UnixMilli(1_786_000_000_000)); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]platformCapabilityProjection {
		t.Helper()
		response := httptest.NewRecorder()
		server.platformInstances(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/platform-instances", nil))
		testassert.Falsef(t, response.Code != http.StatusOK, "platform response = %d %s", response.Code, response.Body.String())
		var body struct {
			Items []platformCapabilityProjection `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		result := make(map[string]platformCapabilityProjection, len(body.Items))
		for _, item := range body.Items {
			testassert.Falsef(t, testassert.Any(func() bool { return item.Name == "FDS 游戏" }, func() bool { return item.Name == "MAME 2003 游戏" }), "retired seed directory remained visible: %#v", item)
			result[item.PlatformID] = item
		}
		return result
	}
	if saturn := read()["saturn"].ImportCapabilities; len(saturn.ContentModes) != 1 || saturn.MultiDisc != nil {
		t.Fatalf("disabled flag capabilities = %#v", saturn)
	}
	server.config.MultiDiscImportEnabled = true
	items := read()
	assertUniquePlatformExtensions(t, items)
	wantExtensions := map[string][]string{
		"virtualboy": {".vb"}, "wonderswan": {".ws", ".wsc"},
		"mastersystem": {".sms"}, "nintendo3ds": {".3ds", ".cci"},
		"arcade": {".zip"}, "dos": {".exe", ".com", ".bat"},
		"nes": {".nes", ".unf", ".unif", ".fds"},
	}
	assertPlatformExtensions(t, items, wantExtensions)
	if saturn := items["saturn"].ImportCapabilities; len(saturn.ContentModes) != 2 ||
		saturn.ContentModes[1] != contentcapability.ModeMultiDisc || saturn.MultiDisc == nil {
		t.Fatalf("Saturn capabilities = %#v", saturn)
	}
	if psx := items["psx"].ImportCapabilities; len(psx.ContentModes) != 1 || psx.MultiDisc != nil {
		t.Fatalf("PSX capabilities = %#v", psx)
	}
	if rpgMaker := items["rpgmaker"].ImportCapabilities; len(rpgMaker.ContentModes) != 1 ||
		rpgMaker.ContentModes[0] != contentcapability.ModeRPGMakerProject || rpgMaker.MultiDisc != nil {
		t.Fatalf("RPG Maker capabilities = %#v", rpgMaker)
	}
}
