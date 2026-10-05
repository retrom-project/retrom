package sourceimport

import (
	"testing"
	"time"

	application "retrom/internal/service/sourceimport"
)

func TestConcurrentWorkersCannotClaimSameJobAndRespectKind(t *testing.T) {
	t.Parallel()
	db := queuedLeaseDatabase(t)
	service := application.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(10) })
	if _, found, err := service.Claim(t.Context(), "IMPORT_SCAN"); err != nil || found {
		t.Fatalf("scan took receive job: %v %v", found, err)
	}
	type result struct {
		found bool
		err   error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	for range 8 {
		go func() {
			<-start
			_, found, err := service.Claim(t.Context(), "IMPORT_RECEIVE")
			results <- result{found, err}
		}()
	}
	close(start)
	claimed := 0
	for range 8 {
		r := <-results
		if r.err != nil {
			t.Error(r.err)
		}
		if r.found {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed one durable job %d times", claimed)
	}
}
