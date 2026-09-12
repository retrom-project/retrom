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

	"retrom/internal/blobstore"
	"retrom/internal/composition"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestReviewCoverRollbackPreservesCauseAndCanReplay(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	fileID := createReviewCoverUpload(t, server)
	cause := errors.New("cover consumption write unavailable")
	enabled := true
	inserted, failed := 0, 0
	faultDB := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.Contains(query, "INSERT INTO review_uploaded_assets") && len(args) > 2 && args[2].Value == fileID {
				count, err := result.RowsAffected()
				if err != nil {
					return nil, err
				}
				inserted += int(count)
			}
			return result, nil
		},
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if enabled && strings.Contains(query, "INSERT INTO upload_consumptions") && strings.Contains(query, "'REVIEW_ASSET'") && len(args) > 2 && args[2].Value == fileID {
				failed++
				return cause
			}
			return nil
		},
	})
	service := composition.NewLibraryReviewCoverUploads(faultDB, server.blobs, server.now)
	request := application.ReviewCoverRequest{ItemID: itemID, UploadFileID: fileID, Kind: "COVER", ExpectedVersion: 1}
	result, err := service.Upload(t.Context(), request)
	if !errors.Is(err, cause) || errors.Is(err, application.ErrReviewCoverConsumed) || result != (application.ReviewCoverResult{}) || inserted != 1 || failed != 1 {
		t.Fatalf("failure lost cause or returned partial success: result=%+v err=%v inserted=%d failed=%d", result, err, inserted, failed)
	}
	assertReviewCoverCounts(t, server, itemID, 0)
	enabled = false
	saved, err := service.Upload(t.Context(), request)
	if err != nil || saved.AssetID == "" || saved.Width != 2 || saved.Height != 3 || saved.MediaType != "image/png" || saved.Version != 1 {
		t.Fatalf("retry=%+v err=%v", saved, err)
	}
	assertReviewCoverCounts(t, server, itemID, 1)
	server.reviewCoverUploads = service
	response := requestReviewCover(t, server, itemID, fileID, `"v1"`)
	var replay struct {
		application.ReviewCoverResult
		URL string `json:"url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"v1"` || replay.ReviewCoverResult != saved || replay.URL != "/api/v1/admin/review-assets/"+saved.AssetID || inserted != 2 {
		t.Fatalf("replay changed identity/shape or inserted duplicate: status=%d body=%s inserted=%d saved=%+v", response.Code, response.Body.String(), inserted, saved)
	}
	assertReviewCoverCounts(t, server, itemID, 1)
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
		{"draft edit", `UPDATE review_drafts SET version=version+1 WHERE import_item_id=?`, application.ErrReviewCoverVersion},
		{"upload release", `UPDATE upload_files SET state='PURGED',final_blob_id=NULL,payload_released_at_ms=1 WHERE id=?`, application.ErrReviewCoverUploadInvalid},
		{"new reservation", `UPDATE import_items SET review_handoff_kind='EMULATIONSTATION' WHERE id=?`, application.ErrReviewCoverVersion},
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
			service := application.NewReviewCoverUploads(repository.NewReviewCoverUploads(server.database), blobs, server.now)
			result, err := service.Upload(t.Context(), application.ReviewCoverRequest{ItemID: itemID, UploadFileID: fileID, Kind: "COVER", ExpectedVersion: 1})
			if !changed || !errors.Is(err, test.expected) || result != (application.ReviewCoverResult{}) {
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
