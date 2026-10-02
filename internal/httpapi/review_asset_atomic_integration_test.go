//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	librarycomposition "retrom/internal/composition/libraryimport"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestReviewAssetRollbackPreservesCauseAndCanReplay(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	fileID := createReviewAssetUpload(t, server)
	fault := &reviewAssetWriteFault{
		fileID: fileID,
		cause:  errors.New("cover consumption write unavailable"), enabled: true,
	}
	faultDB := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		AfterQuery: fault.afterQuery, BeforeQuery: fault.beforeQuery,
	})
	service := librarycomposition.NewReviewAssetUploads(faultDB, server.contentDeps.Files, server.now)
	request := libraryservice.ReviewAssetRequest{ItemID: itemID, UploadFileID: fileID, Kind: "COVER", ExpectedVersion: 1}
	result, err := service.Upload(t.Context(), request)
	if !errors.Is(err, fault.cause) || errors.Is(err, libraryservice.ErrReviewAssetConsumed) ||
		result != (libraryservice.ReviewAssetResult{}) || fault.inserted != 1 || fault.failed != 1 {
		t.Fatalf("failure lost cause or returned partial success: result=%+v err=%v fault.inserted=%d fault.failed=%d", result, err, fault.inserted, fault.failed)
	}
	assertReviewAssetCounts(t, server, itemID, 0)
	fault.enabled = false
	saved, err := service.Upload(t.Context(), request)
	if err != nil || saved.AssetID == "" || saved.Width == nil || *saved.Width != 2 || saved.Height == nil || *saved.Height != 3 ||
		saved.MediaType != "image/png" || saved.Version != 1 {
		t.Fatalf("retry=%+v err=%v", saved, err)
	}
	assertReviewAssetCounts(t, server, itemID, 1)
	server.reviewDeps.AssetUploads = service
	assertReviewAssetReplay(t, server, itemID, fileID, saved, fault)
}

type reviewAssetBarrierBlobs struct {
	store      *filestore.Store
	beforeOpen func()
}

func (blobs reviewAssetBarrierBlobs) OpenRecord(digest string) (io.ReadCloser, error) {
	blobs.beforeOpen()
	file, err := blobs.store.OpenRecord(digest)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func TestReviewAssetRechecksRealSourceAndDraftAfterCASPreparation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query string
		expected    error
	}{
		{"draft edit", `UPDATE import_items SET review_version=version+1 WHERE id=?`, libraryservice.ErrReviewAssetVersion},
		{"upload release", `UPDATE import_files SET file_record=NULL,released_at_ms=1 WHERE id=?`, libraryservice.ErrReviewAssetUploadInvalid},
		{"concurrent discard", `UPDATE import_items SET state='DISCARDED' WHERE id=?`, libraryservice.ErrReviewAssetVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := newTestServer(t)
			itemID := createReviewSnapshotItem(t, server)
			fileID := createReviewAssetUpload(t, server)
			changed := false
			blobs := reviewAssetBarrierBlobs{store: server.contentDeps.Files, beforeOpen: func() {
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
			service := libraryservice.NewReviewAssetUploads(repository.NewReviewAssetUploads(server.database), blobs, server.now)
			result, err := service.Upload(t.Context(), libraryservice.ReviewAssetRequest{
				ItemID:       itemID,
				UploadFileID: fileID, Kind: "COVER", ExpectedVersion: 1,
			})
			if !changed || !errors.Is(err, test.expected) || result != (libraryservice.ReviewAssetResult{}) {
				t.Fatalf("preparation drift accepted: changed=%v result=%+v err=%v", changed, result, err)
			}
			var retained int
			if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT COUNT(*) FROM review_uploaded_assets WHERE import_item_id=?`, itemID).Scan(&retained); err != nil {
				t.Fatal(err)
			}
			if retained != 0 {
				t.Fatalf("preparation drift retained %d assets", retained)
			}
		})
	}
}

type reviewAssetWriteFault struct {
	fileID           string
	cause            error
	enabled          bool
	inserted, failed int
}

func (fault *reviewAssetWriteFault) afterQuery(
	_ context.Context, query string, args []driver.NamedValue, rows driver.Rows,
) (driver.Rows, error) {
	if strings.Contains(query, "INSERT INTO review_uploaded_assets") && len(args) > 2 && args[2].Value == fault.fileID {
		fault.inserted++
	}
	return rows, nil
}

func (fault *reviewAssetWriteFault) beforeQuery(_ context.Context, query string, args []driver.NamedValue) error {
	if fault.enabled && strings.Contains(query, "INSERT INTO upload_consumptions") &&
		strings.Contains(query, "'REVIEW_ASSET'") && len(args) > 2 && args[2].Value == fault.fileID {
		fault.failed++
		return fault.cause
	}
	return nil
}

func assertReviewAssetReplay(
	t *testing.T, server *testServer, itemID, fileID string, saved libraryservice.ReviewAssetResult, fault *reviewAssetWriteFault,
) {
	t.Helper()
	response := requestReviewAsset(t, server, itemID, fileID)
	var replay struct {
		libraryservice.ReviewAssetResult
		URL string `json:"url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"v1"` || !reflect.DeepEqual(replay.ReviewAssetResult, saved) || replay.URL != "/api/v1/admin/review-assets/"+saved.AssetID || fault.inserted != 2 {
		t.Fatalf("replay changed identity/shape or inserted duplicate: status=%d body=%s inserted=%d saved=%+v", response.Code, response.Body.String(), fault.inserted, saved)
	}
	assertReviewAssetCounts(t, server, itemID, 1)
}

func (blobs reviewAssetBarrierBlobs) CopyTo(ctx context.Context, value, directory,
	name string,
) (filestore.Metadata, error) {
	return blobs.store.CopyTo(ctx, value, directory, name)
}
