//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"strings"
	"testing"

	"retrom/internal/bootstrap/composition"
	"retrom/internal/testkit/testsupport"
)

func TestReviewCoverConsumptionFailureRemainsServerError(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	fileID := createReviewCoverUpload(t, server)
	cause := errors.New("review cover consumption failed")
	inserted, failed := 0, 0
	faultDB := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.Contains(query, "INSERT INTO review_uploaded_assets") {
				count, err := result.RowsAffected()
				if err != nil {
					return nil, err
				}
				inserted += int(count)
			}
			return result, nil
		},
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO upload_consumptions") && strings.Contains(query, "'REVIEW_ASSET'") {
				failed++
				return cause
			}
			return nil
		},
	})
	server.reviewCoverUploads = composition.NewLibraryReviewCoverUploads(faultDB, server.blobs, server.now)
	response := requestReviewCover(t, server, itemID, fileID)
	if response.Code != http.StatusInternalServerError || inserted != 1 || failed != 1 {
		t.Fatalf("consumption SQL failure misclassified: status=%d inserted=%d failed=%d body=%s", response.Code, inserted, failed, response.Body.String())
	}
	assertReviewCoverCounts(t, server, itemID, 0)
}

func TestReviewCoverReservedDraftCannotConsumeUpload(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	fileID := createReviewCoverUpload(t, server)
	if _, err := server.database.ExecContext(t.Context(), `UPDATE import_items SET review_handoff_kind='EMULATIONSTATION' WHERE id=?`, itemID); err != nil {
		t.Fatal(err)
	}
	response := requestReviewCover(t, server, itemID, fileID)
	if response.Code != http.StatusConflict {
		t.Fatalf("reserved review accepted cover before handoff: status=%d body=%s", response.Code, response.Body.String())
	}
	assertReviewCoverCounts(t, server, itemID, 0)
}

func assertReviewCoverCounts(t *testing.T, server *Server, itemID string, want int) {
	t.Helper()
	var assets, consumptions int
	var version int64
	var selected bool
	if err := server.database.QueryRowContext(t.Context(), `
SELECT (SELECT COUNT(*) FROM review_uploaded_assets WHERE import_item_id=?),
(SELECT COUNT(*) FROM upload_consumptions WHERE consumer_type='REVIEW_ASSET'),
d.version,d.cover_uploaded_asset_id IS NOT NULL FROM review_drafts d WHERE d.import_item_id=?`, itemID, itemID).
		Scan(&assets, &consumptions, &version, &selected); err != nil {
		t.Fatal(err)
	}
	if assets != want || consumptions != want || version != 1 || selected {
		t.Fatalf("cover transaction changed unexpected state: assets=%d consumptions=%d version=%d selected=%v", assets, consumptions, version, selected)
	}
}

func TestReviewCoverUploadCannotMoveBetweenReviews(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	firstItemID := createReviewSnapshotItem(t, server)
	secondItemID := createReviewSnapshotItem(t, server)
	if firstItemID == secondItemID {
		t.Fatal("fixture did not create independent reviews")
	}
	fileID := createReviewCoverUpload(t, server)
	first := requestReviewCover(t, server, firstItemID, fileID)
	if first.Code != http.StatusCreated {
		t.Fatalf("first cover=%d %s", first.Code, first.Body.String())
	}
	second := requestReviewCover(t, server, secondItemID, fileID)
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), `"UPLOAD_ALREADY_CONSUMED"`) {
		t.Fatalf("cover moved to second review: %d %s", second.Code, second.Body.String())
	}
	assertReviewCoverCounts(t, server, firstItemID, 1)
	var count int
	if err := server.database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM review_uploaded_assets WHERE import_item_id=?`, secondItemID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second review retained %d assets", count)
	}
}

func TestReviewCoverProjectUploadRemainsIneligible(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	fileID := createReviewCoverUpload(t, server)
	if _, err := server.database.ExecContext(t.Context(), `
UPDATE upload_sessions SET purpose='PROJECT' WHERE id=(SELECT upload_session_id FROM upload_files WHERE id=?)`, fileID); err != nil {
		t.Fatal(err)
	}
	response := requestReviewCover(t, server, itemID, fileID)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"UPLOAD_ALREADY_CONSUMED"`) {
		t.Fatalf("project upload accepted as media: status=%d body=%s", response.Code, response.Body.String())
	}
	assertReviewCoverCounts(t, server, itemID, 0)
}
