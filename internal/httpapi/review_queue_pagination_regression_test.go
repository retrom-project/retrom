package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"retrom/internal/authn"
	"retrom/internal/testsupport"
)

type queueRegressionPage struct {
	Items []struct {
		ItemID      string `json:"itemId"`
		UpdatedAtMS int64  `json:"updatedAtMs"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func seedReviewQueuePagination(t *testing.T, server *Server) []string {
	t.Helper()
	instance := testsupport.MustPlatformInstanceID(t, server.database, "gba/mgba")
	digest := strings.Repeat("a", 64)
	mustExecHTTPTest(t, server.database, `INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('queue-upload','COMPLETE','FILES',3,3,?,100,1,1)`, digest)
	mustExecHTTPTest(t, server.database, `INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
SELECT 'queue-import','queue-upload',instance.id,instance.version,instance.platform_id,instance.default_core_id,binding.provider_id,binding.target_id,'NONE','{}',?,'REVIEW_PENDING',3,3,1,1
FROM platform_instances instance JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id WHERE instance.id=?`, digest, instance)
	ids := make([]string, 0, 3)
	for index, updated := range []int64{10, 20, 30} {
		id := fmt.Sprintf("019b0000-0000-7000-8000-%012d", index+1)
		ids = append(ids, id)
		mustExecHTTPTest(t, server.database, `INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
VALUES(?,'queue-import',?,'REVIEW_PENDING','{"files":[{"logicalName":"queue.gba"}]}',?,'queue',1,1)`, id, fmt.Sprintf("%064d", index+1), digest)
		mustExecHTTPTest(t, server.database, `UPDATE import_items SET target_platform_instance_id=?,metadata_json='{"title":"Queue"}',review_version=1,review_created_at_ms=1,review_updated_at_ms=? WHERE id=?`, instance, updated, id)
	}
	return ids
}

func readReviewQueuePage(t *testing.T, server *Server, query url.Values) queueRegressionPage {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/reviews?"+query.Encode(), nil)
	response := httptest.NewRecorder()
	server.reviews(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("review queue status=%d body=%s", response.Code, response.Body.String())
	}
	var page queueRegressionPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestReviewQueueCursorFollowsDraftOrdering(t *testing.T) {
	t.Parallel()
	for _, sort := range []string{"UPDATED_ASC", "UPDATED_DESC"} {
		t.Run(sort, func(t *testing.T) {
			t.Parallel()
			assertReviewQueuePages(t, sort)
		})
	}
}

func assertReviewQueuePages(t *testing.T, sort string) {
	t.Helper()
	server := newTestServer(t)
	ids := seedReviewQueuePagination(t, server)
	if sort == "UPDATED_DESC" {
		ids[0], ids[2] = ids[2], ids[0]
	}
	query := url.Values{"sort": {sort}, "limit": {"1"}}
	for index, id := range ids {
		page := readReviewQueuePage(t, server, query)
		if len(page.Items) != 1 || page.Items[0].ItemID != id {
			t.Fatalf("page %d expected %s got %#v", index+1, id, page)
		}
		if index == len(ids)-1 {
			if page.NextCursor != nil {
				t.Fatalf("last page has cursor: %#v", page)
			}
		} else {
			if page.NextCursor == nil {
				t.Fatalf("page %d omitted continuation", index+1)
			}
			query.Set("cursor", *page.NextCursor)
		}
	}
}

func TestReviewQueueUpdatedAtUsesDraftTimestamp(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	seedReviewQueuePagination(t, server)
	page := readReviewQueuePage(t, server, url.Values{"limit": {"1"}})
	if len(page.Items) != 1 || page.Items[0].UpdatedAtMS != 10 {
		t.Fatalf("draft update timestamp lost: %#v", page)
	}
}

func TestReviewQueueCursorRemainsBoundToPrincipalAndNormalizedFilters(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	seedReviewQueuePagination(t, server)
	query := url.Values{"q": {"  QUEUE  "}, "limit": {"1"}}
	first := readReviewQueuePage(t, server, query)
	if first.NextCursor == nil {
		t.Fatal("first page omitted cursor")
	}
	query.Set("cursor", *first.NextCursor)
	query.Set("q", "queue")
	second := readReviewQueuePage(t, server, query)
	if len(second.Items) != 1 || second.Items[0].ItemID == first.Items[0].ItemID {
		t.Fatalf("equivalent normalized filter failed: %#v", second)
	}
	for _, change := range []struct{ name, key, value string }{
		{"search", "q", "different"}, {"source", "importJobId", "different"}, {"sort", "sort", "UPDATED_DESC"},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := url.Values{"q": {"queue"}, "limit": {"1"}, "cursor": {*first.NextCursor}}
			changed.Set(change.key, change.value)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/reviews?"+changed.Encode(), nil)
			response := httptest.NewRecorder()
			server.reviews(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_CURSOR") {
				t.Fatalf("cursor filter binding=%d %s", response.Code, response.Body.String())
			}
		})
	}
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "another-user"})
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/admin/reviews?"+query.Encode(), nil)
	response := httptest.NewRecorder()
	server.reviews(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_CURSOR") {
		t.Fatalf("cursor principal binding=%d %s", response.Code, response.Body.String())
	}
}
