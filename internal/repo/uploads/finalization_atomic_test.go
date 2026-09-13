package uploads

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	uploadservice "retrom/internal/service/uploads"
	"retrom/internal/testkit/testsupport"
)

type finalizationCountError struct {
	driver.Result
	cause error
}

func (result finalizationCountError) RowsAffected() (int64, error) { return 0, result.cause }

func TestFinalizationBrokenPartCountFailureRollsBackRepairAndFailure(t *testing.T) {
	fixture := newFinalizationFixture(t)
	session := fixture.upload(t, []byte("bytes"))
	var key string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT storage_key FROM upload_parts WHERE upload_file_id=?`, session.Files[0].ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "tmp", "uploads", filepath.FromSlash(key)), []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.service.Close()
	job := fixture.complete(t, session)
	cause := errors.New("part deletion count unavailable")
	var hits atomic.Int64
	database := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
		if strings.Contains(query, "DELETE FROM upload_parts WHERE upload_file_id=? AND part_no=?") && len(args) == 5 && args[0].Value == session.Files[0].ID {
			hits.Add(1)
			return finalizationCountError{Result: result, cause: cause}, nil
		}
		return result, nil
	}})
	err := uploadservice.New(New(database), fixture.blobs, fixture.root, finalizationNow).Run(t.Context(), job)
	var broken *uploadservice.BrokenPart
	if !errors.Is(err, cause) || !errors.As(err, &broken) || hits.Load() != 1 {
		t.Fatalf("failure cause/count: %v %d", err, hits.Load())
	}
	current, err := fixture.service.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "FINALIZING" || current.Files[0].Received != 5 || len(current.Files[0].Parts) != 1 {
		t.Fatalf("partial failure committed: %+v", current)
	}
	var state string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id=?`, job).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("failed event committed: %s", state)
	}
}
