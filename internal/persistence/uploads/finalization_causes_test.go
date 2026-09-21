package uploads

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	uploadservice "retrom/internal/service/uploads"
	"retrom/internal/testsupport"
)

func TestFinalizationReadAndCountFailuresPreserveCauses(t *testing.T) {
	for _, needle := range []string{"SELECT offset_bytes,size_bytes", "SELECT session.total_files"} {
		t.Run(needle, func(t *testing.T) {
			fixture := newFinalizationFixture(t)
			session := fixture.upload(t, []byte("bytes"))
			fixture.service.Close()
			job := fixture.complete(t, session)
			failure := errors.New("upload database unavailable")
			var hits atomic.Int64
			database := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
				expected := session.ID
				if strings.HasPrefix(needle, "SELECT offset") {
					expected = session.Files[0].ID
				}
				if strings.Contains(query, needle) && len(args) == 1 && args[0].Value == expected && hits.Add(1) == 1 {
					return failure
				}
				return nil
			}})
			worker := uploadservice.New(New(database), fixture.blobs, fixture.root, finalizationNow)
			err := worker.Run(t.Context(), job)
			if !errors.Is(err, failure) || hits.Load() != 1 {
				t.Fatalf("cause/hits: %v %d", err, hits.Load())
			}
			awaitFinalizeState(t, fixture.database, job, "FAILED")
		})
	}
}
