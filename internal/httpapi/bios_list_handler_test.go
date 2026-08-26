package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func TestBIOSFullCatalogCursorTraverses286Items(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	seedHTTPTestCoreArtifact(t, server.database, "bios-page-artifact", "mgba", "data/paging.js", strings.Repeat("0", 64), "{}")
	transaction, err := server.database.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	for index := 0; index < 286; index++ {
		if _, err := transaction.ExecContext(context.Background(), `
INSERT INTO bios_requirements(id,core_id,core_artifact_id,source_kind,dat_machine_name,logical_name,
requirement_mode,condition_code,activation_options_json,catalog_digest,size_bytes,md5,sha1,sha256,
source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path)
VALUES(?, 'mgba','bios-page-artifact','STATIC',NULL,?,'REQUIRED',NULL,NULL,lower(hex(zeroblob(32))),
1,lower(hex(randomblob(16))),NULL,NULL,'https://example.invalid/bios','paging-v1',1,1,1,1,'BIOS_BUNDLE',NULL)
`, fmt.Sprintf("paging-requirement-%03d", index), fmt.Sprintf("bios-%03d.bin", index)); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	type page struct {
		FilteredCount int64 `json:"filteredCount"`
		Items         []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	var cursorValue string
	seen := map[string]struct{}{}
	sizes := []int{}
	for {
		url := "/api/v1/admin/bios?scope=FULL_CATALOG&limit=100"
		if cursorValue != "" {
			url += "&cursor=" + cursorValue
		}
		response := httptest.NewRecorder()
		server.bios(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, url, nil))
		testassert.Falsef(t, response.Code != http.StatusOK, "page = %d %s", response.Code, response.Body.String())
		var body page
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		testassert.Falsef(t, body.FilteredCount != 286, "filtered = %d", body.FilteredCount)
		sizes = append(sizes, len(body.Items))
		for _, item := range body.Items {
			if _, exists := seen[item.ID]; exists {
				t.Fatalf("duplicate %s", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		if body.NextCursor == nil {
			break
		}
		cursorValue = *body.NextCursor
	}
	testassert.Falsef(t, testassert.Any(func() bool { return fmt.Sprint(sizes) != "[100 100 86]" }, func() bool { return len(seen) != 286 }), "pages=%v seen=%d", sizes, len(seen))
	invalid := httptest.NewRecorder()
	server.bios(invalid, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/admin/bios?scope=FULL_CATALOG&quick=OPTIONAL&limit=100&cursor="+cursorValue, nil))
	testassert.Falsef(t, invalid.Code != http.StatusBadRequest, "cross-filter cursor = %d %s", invalid.Code, invalid.Body.String())
}
