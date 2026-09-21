//go:build integration

package metadatascrape_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestPendingMediaDoesNotBlockInitialReviewOrMetadataCompletion(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/v1/Lookup/ByHash" {
			return httpResponse(http.StatusOK, "application/json", `{"id":73,"name":"Media Result","attributes":[{"attributeName":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"logo","link":"/api/v1/images/logo"}]}`), nil
		}
		once.Do(func() { close(entered) })
		<-request.Context().Done()
		return nil, context.Cause(request.Context())
	})
	database, importID := createMediaImport(t, client)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("media download was not dispatched")
	}
	var itemState, jobState, runState string
	err := database.SQL.QueryRowContext(t.Context(), `SELECT i.state,j.state,r.state FROM import_items i
 JOIN metadata_scrape_runs r ON r.import_item_id=i.id JOIN jobs j ON j.id=r.job_id
 WHERE i.import_job_id=?`, importID).Scan(&itemState, &jobState, &runState)
	if err != nil {
		t.Fatal(err)
	}
	if itemState != "REVIEW_PENDING" || jobState != "SUCCEEDED" || runState != "COMPLETED" {
		t.Fatalf("pending media blocked review: item=%s job=%s run=%s", itemState, jobState, runState)
	}
	var jobs int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs j JOIN import_items i ON i.id=j.scope_id WHERE i.import_job_id=? AND j.kind='MEDIA_FETCH'`, importID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("candidate media has %d durable jobs", jobs)
	}
}
