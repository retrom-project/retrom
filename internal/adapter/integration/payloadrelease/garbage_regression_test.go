package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	payloadreleasemodel "retrom/internal/model/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
	"retrom/internal/testkit/testsupport"
)

func claimedGarbage(t *testing.T) (gcSchedulingFixture, claimedJob) {
	t.Helper()
	fixture := newGCSchedulingFixture(t)
	if _, err := fixture.service.ScheduleImmediateGC(t.Context(), "manual-gc-user"); err != nil {
		t.Fatal(err)
	}
	job, found, err := fixture.service.claim(t.Context())
	if err != nil || !found || job.ScopeID != "manual-gc-blob" {
		t.Fatalf("claim garbage: %+v/%t/%v", job, found, err)
	}
	return fixture, job
}

func TestGarbagePreservesCatalogAndBytesWhenDeleteCountFails(t *testing.T) {
	t.Parallel()
	fixture, job := claimedGarbage(t)
	cause := errors.New("garbage affected-row failure")
	var hits atomic.Int64
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "DELETE FROM blobs WHERE") &&
				gcBoundArgument(args, job.ScopeID) {
				hits.Add(1)
				return failedSchedulingCount{Result: result, cause: cause}, nil
			}
			return result, nil
		},
	})
	err := gcFaultService(t, fixture, fault).execute(t.Context(), job)
	var blobs, candidates int
	readErr := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM blobs WHERE id='manual-gc-blob'),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob')`).Scan(&blobs, &candidates)
	_, physicalErr := os.Stat(fixture.blobs.Path(job.Input.Inputs.SHA256))
	if !errors.Is(err, cause) || hits.Load() != 1 || readErr != nil || blobs != 1 || candidates != 1 || physicalErr != nil {
		t.Fatalf("unconfirmed delete escaped: error=%v hits=%d blobs=%d candidates=%d read=%v physical=%v",
			err, hits.Load(), blobs, candidates, readErr, physicalErr)
	}
}

func obstructGarbageFile(t *testing.T, filename string) {
	t.Helper()
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filename, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filename, "obstruction"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGarbagePhysicalFailureRetainsFilesystemCause(t *testing.T) {
	t.Parallel()
	fixture, job := claimedGarbage(t)
	obstructGarbageFile(t, fixture.blobs.Path(job.Input.Inputs.SHA256))
	err := fixture.service.execute(t.Context(), job)
	if !errors.Is(err, syscall.ENOTEMPTY) || payloadreleaseservice.WorkErrorCode(err) != "BLOB_GC_PHYSICAL_DELETE_FAILED" {
		t.Fatalf("physical garbage error lost its cause or domain code: %v", err)
	}
}

func TestGarbageRetryKeepsReRegisteredDigestOwnedByAnotherBlob(t *testing.T) {
	t.Parallel()
	fixture, job := claimedGarbage(t)
	filename := fixture.blobs.Path(job.Input.Inputs.SHA256)
	obstructGarbageFile(t, filename)
	if err := fixture.service.execute(t.Context(), job); payloadreleaseservice.WorkErrorCode(err) != "BLOB_GC_PHYSICAL_DELETE_FAILED" {
		t.Fatalf("expected failed physical removal: %v", err)
	}
	if err := os.Remove(filepath.Join(filename, "obstruction")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	metadata, err := fixture.blobs.Put(strings.NewReader("GC scheduling reference"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.database.ExecContext(t.Context(), `INSERT INTO blobs
 (id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
 VALUES('replacement-garbage-blob',?,?,?,?,?,'application/octet-stream',10)`,
		metadata.SHA256, metadata.Size, metadata.MD5, metadata.SHA1, metadata.CRC32)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.database.ExecContext(t.Context(), `INSERT INTO game_assets
 (id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
 VALUES('replacement-garbage-owner','schedule-game','replacement-garbage-blob','COVER',0,1,1,'image/png',10)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.execute(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filename)
	if err != nil || string(content) != "GC scheduling reference" {
		t.Fatalf("old garbage retry destroyed new referenced digest: content=%q error=%v", content, err)
	}
}

func TestGarbageCancelledCandidateCannotBypassNewRetention(t *testing.T) {
	t.Parallel()
	fixture, job := claimedGarbage(t)
	_, err := fixture.database.ExecContext(t.Context(), `INSERT INTO game_assets
 (id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
 VALUES('transient-garbage-owner','schedule-game','manual-gc-blob','COVER',0,1,1,'image/png',10)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.stageAllUnreferenced(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(t.Context(), `DELETE FROM game_assets WHERE id='transient-garbage-owner'`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.execute(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	var blobs int
	err = fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs WHERE id='manual-gc-blob'`).Scan(&blobs)
	_, physicalErr := os.Stat(fixture.blobs.Path(job.Input.Inputs.SHA256))
	if err != nil || blobs != 1 || physicalErr != nil {
		t.Fatalf("cancelled candidate bypassed retention: blobs=%d read=%v physical=%v", blobs, err, physicalErr)
	}
}

func TestGarbageRollsBackWhenOriginalLeaseExpiresAfterCatalogDelete(t *testing.T) {
	t.Parallel()
	fixture, job := claimedGarbage(t)
	var now, hits atomic.Int64
	now.Store(10)
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "DELETE FROM blobs WHERE") &&
				gcBoundArgument(args, job.ScopeID) {
				hits.Add(1)
				now.Store(job.Work.Lease.Value + 1)
			}
			return result, nil
		},
	})
	service, err := New(fault, fixture.blobs, func() time.Time { return time.UnixMilli(now.Load()) }, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	err = service.execute(t.Context(), job)
	var blobs, candidates int
	readErr := fixture.database.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM blobs WHERE id='manual-gc-blob'),
 (SELECT count(*) FROM blob_gc_candidates WHERE blob_id='manual-gc-blob')`).Scan(&blobs, &candidates)
	_, physicalErr := os.Stat(fixture.blobs.Path(job.Input.Inputs.SHA256))
	if !errors.Is(err, payloadreleasemodel.ErrExecutionLost) || hits.Load() != 1 || readErr != nil ||
		blobs != 1 || candidates != 1 || physicalErr != nil {
		t.Fatalf("expired garbage escaped: error=%v hits=%d blobs=%d candidates=%d read=%v physical=%v",
			err, hits.Load(), blobs, candidates, readErr, physicalErr)
	}
}
