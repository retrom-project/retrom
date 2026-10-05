package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	dbapi "retrom/internal/database"

	"github.com/google/uuid"
)

func TestIndependentConcurrentDirectoryCreatesReserveUniqueSlugs(t *testing.T) {
	server, create := directoryCreationProbe(t)
	start := make(chan struct{})
	responses := make([]*httptest.ResponseRecorder, 8)
	keys := make([]string, len(responses))
	var workers sync.WaitGroup
	for index := range responses {
		keys[index] = uuid.NewString()
		workers.Go(func() { <-start; responses[index] = create(keys[index], "ConcurrentSlugProbe") })
	}
	close(start)
	workers.Wait()
	ids, slugs := map[string]bool{}, map[string]bool{}
	for index, response := range responses {
		if response.Code != http.StatusCreated {
			t.Fatalf("create %d: %d %s", index, response.Code, response.Body.String())
		}
		var directory struct{ ID, Slug string }
		if err := json.Unmarshal(response.Body.Bytes(), &directory); err != nil {
			t.Fatal(err)
		}
		if directory.ID == "" || directory.Slug == "" || ids[directory.ID] || slugs[directory.Slug] {
			t.Fatalf("duplicate or absent identity: %+v", directory)
		}
		ids[directory.ID], slugs[directory.Slug] = true, true
		assertDirectoryReplay(t, response, create(keys[index], "ConcurrentSlugProbe"))
	}
	var audits, receipts int
	err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT
  (SELECT count(*) FROM audit_events WHERE action='PLATFORM_INSTANCE_CREATED'
   AND resource_id IN (SELECT id FROM platform_instances WHERE name='ConcurrentSlugProbe')),
  (SELECT count(*) FROM idempotency_records WHERE operation_id='platforminstance.create')`).Scan(&audits, &receipts)
	if err != nil {
		t.Fatal(err)
	}
	if audits != 8 || receipts != 8 {
		t.Fatalf("audits=%d receipts=%d", audits, receipts)
	}
}
