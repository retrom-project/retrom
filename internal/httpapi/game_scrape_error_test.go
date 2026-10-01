package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"retrom/internal/service/metadatascrape"
)

type scrapeScheduleFailure struct{ cause error }

func (failure scrapeScheduleFailure) WithWrite(context.Context, func(metadatascrape.ScheduleScope) error) error {
	return failure.cause
}

func TestGameScrapeMapsIndexVersionAndStorageFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		status int
		code   string
	}{
		{"index missing", metadatascrape.ErrArchiveIndexMissing, 409, "METADATA_ARCHIVE_INDEX_MISSING"},
		{"stale version", metadatascrape.ErrGameVersionConflict, 409, "VERSION_CONFLICT"},
		{"storage failure", errors.New("storage unavailable"), 500, "INTERNAL_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t)
			server.reviewDeps.Metadata = metadatascrape.New(scrapeScheduleFailure{cause: test.cause}, nil, time.Now)
			t.Cleanup(server.reviewDeps.Metadata.Close)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
				"/api/v1/admin/games/01980000-0000-7000-8000-000000000101/scrape-candidates",
				strings.NewReader(`{"metadataProvider":"HASHEOUS"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("If-Match", `"v1"`)
			request.Header.Set("Idempotency-Key", "01980000-0000-7000-8000-000000000201")
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
