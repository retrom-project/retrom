package payloadrelease

import (
	"errors"
	"testing"
	"time"
)

func TestPayloadInitializationRejectsUnrelatedReleaseJob(t *testing.T) {
	t.Parallel()
	for _, change := range []struct{ name, query string }{
		{"scope id", `UPDATE jobs SET scope_id='another-game' WHERE id=?`},
		{"scope type", `UPDATE jobs SET scope_type='IMPORT_ITEM' WHERE id=?`},
		{"job kind", `UPDATE jobs SET kind='BLOB_GC',scope_type='BLOB' WHERE id=?`},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			fixture := queuedReleaseWorker(t)
			if _, err := fixture.database.ExecContext(t.Context(), change.query, fixture.jobID); err != nil {
				t.Fatal(err)
			}
			service, err := New(fixture.database, nil, fixture.service.now, 7*24*time.Hour)
			if service != nil {
				service.Close()
			}
			if !errors.Is(err, ErrLifecycleInvariant) || service != nil {
				t.Fatalf("unrelated release job accepted: service=%t error=%v", service != nil, err)
			}
		})
	}
}
