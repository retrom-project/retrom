package uploads

import (
	"context"
	"database/sql/driver"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	uploadservice "retrom/internal/service/uploads"
	"retrom/internal/testsupport"
)

func TestFinalizationRetriesSerializationWithoutRepeatingFileAssembly(t *testing.T) {
	fixture := newFinalizationFixture(t)
	session := fixture.upload(t, []byte("bytes"))
	fixture.service.Close()
	job := fixture.complete(t, session)
	var conflicted atomic.Bool
	database := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(ctx context.Context, query string, _ []driver.NamedValue) error {
			if !strings.Contains(query, "UPDATE upload_files") || !strings.Contains(query, "state='COMPLETE'") ||
				!conflicted.CompareAndSwap(false, true) {
				return nil
			}
			// Commit a competing row version after finalization's authority reads.
			// PostgreSQL must reject publication from that stale transaction snapshot.
			_, err := fixture.database.ExecContext(ctx,
				"UPDATE upload_files SET updated_at_ms=updated_at_ms WHERE id=?", session.Files[0].ID)
			return err
		},
	})
	var assemblies atomic.Int64
	worker := uploadservice.New(New(database), finalizationBlobs{
		store: fixture.blobs,
		put: func(reader io.Reader) (filestore.Metadata, error) {
			assemblies.Add(1)
			return fixture.blobs.Put(reader)
		},
	}, fixture.root, finalizationNow)
	t.Cleanup(worker.Close)
	if err := worker.Run(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if !conflicted.Load() || assemblies.Load() != 1 {
		t.Fatalf("conflicted=%v file assemblies=%d", conflicted.Load(), assemblies.Load())
	}
	var state string
	var attempts int
	if err := dbapi.QueryRowContext(t.Context(), fixture.database,
		"SELECT state,attempt_count FROM jobs WHERE id=?", job).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "SUCCEEDED" || attempts != 1 {
		t.Fatalf("finalization state=%s attempts=%d", state, attempts)
	}
}
