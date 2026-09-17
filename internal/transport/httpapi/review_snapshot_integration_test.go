//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"retrom/internal/adapter/integration/libraryimport"
	"retrom/internal/bootstrap/composition"
	"retrom/internal/capability/security/authn"
	libraryservice "retrom/internal/model/libraryimport"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/testkit/testsupport"
)

func TestReviewDetailUsesOneSnapshotAcrossDraftAndTags(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	tag, err := server.tagService.Create(t.Context(), "01980000-0000-7000-8000-000000009999", "Later tag")
	if err != nil {
		t.Fatal(err)
	}
	var beforeTitle string
	var beforeVersion int64
	if err := server.database.QueryRowContext(t.Context(), `
SELECT json_extract(metadata_json,'$.title'),version FROM review_drafts WHERE import_item_id=?`, itemID).
		Scan(&beforeTitle, &beforeVersion); err != nil {
		t.Fatal(err)
	}
	var barrier sync.Once
	changed := false
	database := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(ctx context.Context, query string, _ []driver.NamedValue) error {
			if !strings.Contains(query, "FROM scrape_candidates c") {
				return nil
			}
			var mutationErr error
			barrier.Do(func() {
				title := "Later title"
				_, mutationErr = server.importer.PatchDraft(authn.WithPrincipal(ctx, authn.Principal{UserID: "01980000-0000-7000-8000-000000009999"}), itemID, beforeVersion, libraryimport.DraftPatch{
					Metadata: &libraryimport.MetadataPatch{Title: &title}, TagIDs: []string{tag.TagID},
				})
				changed = mutationErr == nil
			})
			return mutationErr
		},
	})
	server.reviewDetails = composition.NewLibraryReviewDetails(database)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	request.SetPathValue("importItemId", itemID)
	recorder := httptest.NewRecorder()
	server.review(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", recorder.Code, recorder.Body.String())
	}
	if !changed {
		t.Fatal("concurrent legal draft mutation did not run after headline query")
	}
	var result struct {
		Version  int64 `json:"version"`
		Metadata struct {
			Title string `json:"title"`
		} `json:"metadata"`
		Tags []struct {
			TagID string `json:"tagId"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if recorder.Header().Get("ETag") != fmt.Sprintf(`"v%d"`, beforeVersion) {
		t.Fatalf("mixed ETag = %q", recorder.Header().Get("ETag"))
	}
	if result.Version != beforeVersion || result.Metadata.Title != beforeTitle || len(result.Tags) != 0 {
		t.Fatalf("mixed review snapshot: version=%d title=%q tags=%+v; expected version=%d title=%q with no tags",
			result.Version, result.Metadata.Title, result.Tags, beforeVersion, beforeTitle)
	}
	assertNextReviewSnapshot(t, server, itemID, tag.TagID, beforeVersion)
}

func createReviewSnapshotItem(t *testing.T, server *Server) string {
	t.Helper()
	metadata, err := server.blobs.Put(bytes.NewReader([]byte("Retrom owned review snapshot fixture")))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobcatalog.EnsureRecord(t.Context(), server.database, metadata, "application/octet-stream", 0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.importer.CreateServerSource(t.Context(),
		testsupport.MustPlatformInstanceID(t, server.database, "gba/mgba"), "STANDARD",
		[]libraryimport.ServerSourceFile{{RelativePath: "Snapshot.gba", BlobID: blobID, SizeBytes: metadata.Size}}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].State != "REVIEW_PENDING" {
		t.Fatalf("review fixture = %+v", result)
	}
	return result.Items[0].ItemID
}

func TestReviewDetailInvisibleHeadlinePrecedesChildReads(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	if _, err := server.database.ExecContext(t.Context(),
		`UPDATE import_items SET review_handoff_kind='EMULATIONSTATION' WHERE id=?`, itemID); err != nil {
		t.Fatal(err)
	}
	queries := 0
	database := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			queries++
			if !strings.Contains(query, "FROM import_items i") {
				return errors.New("invisible review accessed child data")
			}
			return nil
		},
	})
	server.reviewDetails = composition.NewLibraryReviewDetails(database)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	request.SetPathValue("importItemId", itemID)
	response := httptest.NewRecorder()
	server.review(response, request)
	if response.Code != http.StatusNotFound || queries != 1 {
		t.Fatalf("hidden detail = %d, queries=%d: %s", response.Code, queries, response.Body.String())
	}
}

func TestReviewDetailPreservesLateSQLFailureAndClearsProjection(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	cause := errors.New("review tags query failed")
	calls := 0
	database := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "FROM review_draft_tags relation") {
				calls++
				return cause
			}
			return nil
		},
	})
	reader := composition.NewLibraryReviewDetails(database)
	result, err := reader.Get(t.Context(), itemID)
	if !errors.Is(err, cause) || !reflect.DeepEqual(result, libraryservice.ReviewDetail{}) || calls != 1 {
		t.Fatalf("late SQL failure: calls=%d result=%+v err=%v", calls, result, err)
	}
	server.reviewDetails = reader
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	request.SetPathValue("importItemId", itemID)
	response := httptest.NewRecorder()
	server.review(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "Snapshot") || response.Header().Get("ETag") != "" {
		t.Fatalf("partial detail escaped: %d %s", response.Code, response.Body.String())
	}
}

func assertNextReviewSnapshot(t *testing.T, server *Server, itemID, tagID string, beforeVersion int64) {
	t.Helper()

	latest, err := server.reviewDetails.Get(t.Context(), itemID)
	if err != nil || latest.Version != beforeVersion+1 || len(latest.Tags) != 1 || latest.Tags[0].TagID != tagID ||
		!strings.Contains(string(latest.Metadata), "Later title") {
		t.Fatalf("next snapshot did not observe committed mutation: %+v %v", latest, err)
	}
}

func TestReviewDetailRetainsPublishedDuplicateProjection(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	originalID := createReviewSnapshotItem(t, server)
	reviewID := createReviewSnapshotItem(t, server)
	published, err := server.importer.Approve(t.Context(), originalID, 1)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := server.reviewDetails.Get(t.Context(), reviewID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.DuplicateGames) != 1 || detail.DuplicateGames[0].GameID != published.GameID || len(detail.ContentIdentityDigest) != 64 {
		t.Fatalf("published duplicate projection=%+v digest=%q", detail.DuplicateGames, detail.ContentIdentityDigest)
	}
}
