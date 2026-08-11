package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/contentcapability"
)

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

func TestCreateImportContentModeDefaultsToStandardAndMapsMultiDiscAdmissionErrors(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	now := time.UnixMilli(1_786_000_000_000)
	if err := server.dependencies.Bootstrap(context.Background(), server.database, now); err != nil {
		t.Fatal(err)
	}
	server.importer.WithMultiDiscImportEnabled(true)
	metadata, err := server.blobs.Put(strings.NewReader("MComprHDdeterministic CHD fixture"))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobstore.EnsureRecord(t.Context(), server.database, metadata, "application/octet-stream", now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	createUpload := func(uploadID, fileID string) {
		t.Helper()
		if _, err := server.database.Exec(`
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,version,
expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','DIRECTORY',1,?, ?,1,?,?,?)
`, uploadID, metadata.Size, strings.Repeat("a", 64), now.Add(time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		if _, err := server.database.Exec(`
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?,'game.chd',?,?,?,'COMPLETE',?,?)
`, fileID, uploadID, metadata.Size, metadata.Size, blobID, now.UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	const firstUpload = "01980000-0000-7000-8000-000000007101"
	const secondUpload = "01980000-0000-7000-8000-000000007102"
	createUpload(firstUpload, "01980000-0000-7000-8000-000000007111")
	createUpload(secondUpload, "01980000-0000-7000-8000-000000007112")
	send := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/imports", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.createImport(response, request)
		return response
	}
	missing := send(`{"uploadId":"` + firstUpload + `","targetPlatformInstanceId":"01980000-0000-7000-8000-000000000020","metadataProvider":"NONE","contentMode":"MULTI_DISC_M3U_V1"}`)
	if missing.Code != http.StatusUnprocessableEntity || !strings.Contains(missing.Body.String(), "MULTI_DISC_PLAYLIST_MISSING") {
		t.Fatalf("missing playlist = %d %s", missing.Code, missing.Body.String())
	}
	unsupported := send(`{"uploadId":"` + firstUpload + `","targetPlatformInstanceId":"01980000-0000-7000-8000-000000000019","metadataProvider":"NONE","contentMode":"MULTI_DISC_M3U_V1"}`)
	if unsupported.Code != http.StatusUnprocessableEntity || !strings.Contains(unsupported.Body.String(), "MULTI_DISC_MODE_UNAVAILABLE") {
		t.Fatalf("unsupported target = %d %s", unsupported.Code, unsupported.Body.String())
	}
	omitted := send(`{"uploadId":"` + firstUpload + `","targetPlatformInstanceId":"01980000-0000-7000-8000-000000000020","metadataProvider":"NONE"}`)
	explicit := send(`{"uploadId":"` + secondUpload + `","targetPlatformInstanceId":"01980000-0000-7000-8000-000000000020","metadataProvider":"NONE","contentMode":"STANDARD"}`)
	if omitted.Code != http.StatusAccepted || explicit.Code != http.StatusAccepted {
		t.Fatalf("standard admission omitted=%d %s explicit=%d %s", omitted.Code, omitted.Body.String(), explicit.Code, explicit.Body.String())
	}
	var omittedConfig, explicitConfig, omittedDigest, explicitDigest string
	if err := server.database.QueryRow(`SELECT config_snapshot_json,config_snapshot_digest FROM import_jobs WHERE upload_session_id=?`, firstUpload).
		Scan(&omittedConfig, &omittedDigest); err != nil {
		t.Fatal(err)
	}
	if err := server.database.QueryRow(`SELECT config_snapshot_json,config_snapshot_digest FROM import_jobs WHERE upload_session_id=?`, secondUpload).
		Scan(&explicitConfig, &explicitDigest); err != nil {
		t.Fatal(err)
	}
	if omittedConfig != explicitConfig || omittedDigest != explicitDigest ||
		!strings.Contains(omittedConfig, `"contentMode":"STANDARD"`) {
		t.Fatalf("default/explicit snapshots differ: %s/%s digest=%s/%s", omittedConfig, explicitConfig, omittedDigest, explicitDigest)
	}
}

func TestPlatformSlugSuffixStaysWithinStorageLimit(t *testing.T) {
	t.Parallel()
	base := strings.Repeat("a", 80)
	if got := platformSlugWithSuffix(base, 12); len(got) != 80 || !strings.HasSuffix(got, "-12") {
		t.Fatalf("suffixed slug = %q (%d bytes)", got, len(got))
	}
}

func TestPlatformImportCapabilitiesUseFeaturePlatformAndArtifactIntersection(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	if err := server.dependencies.Bootstrap(t.Context(), server.database, time.UnixMilli(1_786_000_000_000)); err != nil {
		t.Fatal(err)
	}
	type platform struct {
		PlatformID          string                               `json:"platformId"`
		SupportedExtensions []string                             `json:"supportedExtensions"`
		ImportCapabilities  contentcapability.ImportCapabilities `json:"importCapabilities"`
	}
	read := func() map[string]platform {
		t.Helper()
		response := httptest.NewRecorder()
		server.platformInstances(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/platform-instances", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("platform response = %d %s", response.Code, response.Body.String())
		}
		var body struct {
			Items []platform `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		result := make(map[string]platform, len(body.Items))
		for _, item := range body.Items {
			result[item.PlatformID] = item
		}
		return result
	}
	if saturn := read()["saturn"].ImportCapabilities; len(saturn.ContentModes) != 1 || saturn.MultiDisc != nil {
		t.Fatalf("disabled flag capabilities = %#v", saturn)
	}
	server.config.MultiDiscImportEnabled = true
	items := read()
	for platformID, item := range items {
		if len(item.SupportedExtensions) == 0 {
			t.Fatalf("%s has no supported extensions", platformID)
		}
	}
	wantExtensions := map[string][]string{
		"virtualboy": {".vb"}, "wonderswan": {".ws", ".wsc"},
		"mastersystem": {".sms"}, "nintendo3ds": {".3ds", ".cci"},
		"arcade": {".zip"}, "dos": {".exe", ".com", ".bat"},
	}
	for platformID, want := range wantExtensions {
		got := items[platformID].SupportedExtensions
		if len(got) != len(want) {
			t.Fatalf("%s extensions = %#v, want %#v", platformID, got, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("%s extensions = %#v, want %#v", platformID, got, want)
			}
		}
	}
	if saturn := items["saturn"].ImportCapabilities; len(saturn.ContentModes) != 2 ||
		saturn.ContentModes[1] != contentcapability.ModeMultiDiscM3UV1 || saturn.MultiDisc == nil {
		t.Fatalf("Saturn capabilities = %#v", saturn)
	}
	if psx := items["psx"].ImportCapabilities; len(psx.ContentModes) != 1 || psx.MultiDisc != nil {
		t.Fatalf("PSX capabilities = %#v", psx)
	}
}
