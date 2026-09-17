//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/bootstrap/composition"
	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

func TestReviewCoverRollbackPreservesCauseAndCanReplay(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	fileID := createReviewCoverUpload(t, server)
	fault := &reviewCoverWriteFault{fileID: fileID, cause: errors.New("cover consumption write unavailable"), enabled: true}
	faultDB := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		AfterExec: fault.afterExec, BeforeQuery: fault.beforeQuery,
	})
	service := composition.NewLibraryReviewCoverUploads(faultDB, server.blobs, server.now)
	request := libraryimportmodel.ReviewCoverRequest{ItemID: itemID, UploadFileID: fileID, Kind: "COVER", ExpectedVersion: 1}
	result, err := service.Upload(t.Context(), request)
	if !errors.Is(err, fault.cause) || errors.Is(err, libraryimportmodel.ErrReviewCoverConsumed) || result != (libraryimportmodel.ReviewCoverResult{}) || fault.inserted != 1 || fault.failed != 1 {
		t.Fatalf("failure lost cause or returned partial success: result=%+v err=%v fault.inserted=%d fault.failed=%d", result, err, fault.inserted, fault.failed)
	}
	assertReviewCoverCounts(t, server, itemID, 0)
	fault.enabled = false
	saved, err := service.Upload(t.Context(), request)
	if err != nil || saved.AssetID == "" || saved.Width != 2 || saved.Height != 3 || saved.MediaType != "image/png" || saved.Version != 1 {
		t.Fatalf("retry=%+v err=%v", saved, err)
	}
	assertReviewCoverCounts(t, server, itemID, 1)
	server.reviewCoverUploads = service
	assertReviewCoverReplay(t, server, itemID, fileID, saved, fault)
}

type reviewCoverBarrierBlobs struct {
	store      *blobstore.Store
	beforeOpen func()
}

func (blobs reviewCoverBarrierBlobs) OpenDigest(digest string) (io.ReadCloser, error) {
	blobs.beforeOpen()
	file, err := blobs.store.OpenDigest(digest)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func TestReviewCoverRechecksRealSourceAndDraftAfterCASPreparation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query string
		expected    error
	}{
		{"draft edit", `UPDATE review_drafts SET version=version+1 WHERE import_item_id=?`, libraryimportmodel.ErrReviewCoverVersion},
		{"upload release", `UPDATE upload_files SET state='PURGED',final_blob_id=NULL,payload_released_at_ms=1 WHERE id=?`, libraryimportmodel.ErrReviewCoverUploadInvalid},
		{"new reservation", `UPDATE import_items SET review_handoff_kind='EMULATIONSTATION' WHERE id=?`, libraryimportmodel.ErrReviewCoverVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := newTestServer(t)
			itemID := createReviewSnapshotItem(t, server)
			fileID := createReviewCoverUpload(t, server)
			changed := false
			blobs := reviewCoverBarrierBlobs{store: server.blobs, beforeOpen: func() {
				id := itemID
				if test.name == "upload release" {
					id = fileID
				}
				result, err := server.database.ExecContext(t.Context(), test.query, id)
				if err != nil {
					t.Fatal(err)
				}
				count, err := result.RowsAffected()
				if err != nil {
					t.Fatal(err)
				}
				changed = count == 1
			}}
			service := libraryimportservice.NewReviewCoverUploads(repository.NewReviewCoverUploads(server.database), blobs, server.now)
			result, err := service.Upload(t.Context(), libraryimportmodel.ReviewCoverRequest{ItemID: itemID, UploadFileID: fileID, Kind: "COVER", ExpectedVersion: 1})
			if !changed || !errors.Is(err, test.expected) || result != (libraryimportmodel.ReviewCoverResult{}) {
				t.Fatalf("preparation drift accepted: changed=%v result=%+v err=%v", changed, result, err)
			}
			var retained int
			if err := server.database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM review_uploaded_assets WHERE import_item_id=?`, itemID).Scan(&retained); err != nil {
				t.Fatal(err)
			}
			if retained != 0 {
				t.Fatalf("preparation drift retained %d assets", retained)
			}
		})
	}
}

type reviewCoverWriteFault struct {
	fileID           string
	cause            error
	enabled          bool
	inserted, failed int
}

func (fault *reviewCoverWriteFault) afterExec(
	_ context.Context, query string, args []driver.NamedValue, result driver.Result,
) (driver.Result, error) {
	if strings.Contains(query, "INSERT INTO review_uploaded_assets") && len(args) > 2 && args[2].Value == fault.fileID {
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		fault.inserted += int(count)
	}
	return result, nil
}

func (fault *reviewCoverWriteFault) beforeQuery(_ context.Context, query string, args []driver.NamedValue) error {
	if fault.enabled && strings.Contains(query, "INSERT INTO upload_consumptions") && strings.Contains(query, "'REVIEW_ASSET'") && len(args) > 2 && args[2].Value == fault.fileID {
		fault.failed++
		return fault.cause
	}
	return nil
}

func assertReviewCoverReplay(
	t *testing.T, server *Server, itemID, fileID string, saved libraryimportmodel.ReviewCoverResult, fault *reviewCoverWriteFault,
) {
	t.Helper()
	response := requestReviewCover(t, server, itemID, fileID)
	var replay struct {
		libraryimportmodel.ReviewCoverResult
		URL string `json:"url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"v1"` || replay.ReviewCoverResult != saved || replay.URL != "/api/v1/admin/review-assets/"+saved.AssetID || fault.inserted != 2 {
		t.Fatalf("replay changed identity/shape or inserted duplicate: status=%d body=%s inserted=%d saved=%+v", response.Code, response.Body.String(), fault.inserted, saved)
	}
	assertReviewCoverCounts(t, server, itemID, 1)
}
