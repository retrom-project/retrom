package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		PlatformID         string                               `json:"platformId"`
		ImportCapabilities contentcapability.ImportCapabilities `json:"importCapabilities"`
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
	if saturn := items["saturn"].ImportCapabilities; len(saturn.ContentModes) != 2 ||
		saturn.ContentModes[1] != contentcapability.ModeMultiDiscM3UV1 || saturn.MultiDisc == nil {
		t.Fatalf("Saturn capabilities = %#v", saturn)
	}
	if psx := items["psx"].ImportCapabilities; len(psx.ContentModes) != 1 || psx.MultiDisc != nil {
		t.Fatalf("PSX capabilities = %#v", psx)
	}
}
