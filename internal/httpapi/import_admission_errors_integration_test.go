//go:build integration

package httpapi

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/composition"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestImportAdmissionStorageFailureReturns500(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	itemID := createReviewSnapshotItem(t, server)
	var uploadID, targetID string
	err := server.database.QueryRowContext(t.Context(), `SELECT parent.upload_session_id,parent.target_platform_instance_id
 FROM import_items item JOIN import_jobs parent ON parent.id=item.import_job_id WHERE item.id=?`, itemID).Scan(&uploadID, &targetID)
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	database := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "FROM upload_sessions") {
				hits++
				return errors.New("queue facts unavailable")
			}
			return nil
		},
	})
	server.importAdmissions = composition.NewLibraryImportAdmissions(database, nil, libraryservice.ImportAdmissionOptions{Now: server.now})
	body := fmt.Sprintf(`{"uploadId":%q,"targetPlatformInstanceId":%q,"metadataProvider":"NONE","tagIds":[]}`, uploadID, targetID)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/imports", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.createImport(response, request)
	if response.Code != http.StatusInternalServerError || hits != 1 {
		t.Fatalf("response=%d %s hits=%d", response.Code, response.Body.String(), hits)
	}
}
