//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/testkit/testsupport"
)

func admissionFixture(t *testing.T) (*Service, CreateRequest) {
	t.Helper()
	database, blobs, dataDir := openImportGroupFixture(t, t.Context())
	uploadID := completeImportGroupUpload(t, t.Context(), database.SQL, blobs, dataDir, onsProjectArchive(t))
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	return service, CreateRequest{
		UploadID:                 uploadID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "ons/onscripter_yuri"),
		MetadataProvider:         "NONE",
		ContentMode:              "ONS_PROJECT",
	}
}

func TestImportAdmissionPreservesReadFailure(t *testing.T) {
	t.Parallel()
	for _, match := range []string{"FROM upload_sessions", "FROM platform_instances pi", "FROM upload_files f"} {
		t.Run(match, func(t *testing.T) {
			t.Parallel()
			service, request := admissionFixture(t)
			cause := errors.New("admission facts unavailable")
			hits := 0
			service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if strings.Contains(query, match) {
						hits++
						return cause
					}
					return nil
				},
			})
			result, err := service.QueueCreate(t.Context(), request)
			if !errors.Is(err, cause) || errors.Is(err, ErrInvalid) || result != (Created{}) || hits != 1 {
				t.Fatalf("result=%+v err=%v hits=%d", result, err, hits)
			}
		})
	}
}

func TestImportAdmissionRejectsTargetDisabledBeforeQueueWrite(t *testing.T) {
	service, request := admissionFixture(t)
	sourceDB := service.database
	hits := 0
	var queued string
	service.database = testsupport.OpenSQLFaultDatabase(
		t,
		sourceDB,
		testsupport.SQLFaultHooks{
			BeforeExec: func(ctx context.Context, query string, _ []driver.NamedValue) error {
				if !strings.Contains(query, "UPDATE upload_sessions SET version=version") {
					return nil
				}
				hits++
				_, err := sourceDB.ExecContext(
					ctx,
					`UPDATE platform_instances SET enabled=0,version=version+1 WHERE id=?`,
					request.TargetPlatformInstanceID,
				)
				return err
			},
		},
	)
	release := gateImportWorker(t, service)
	t.Cleanup(func() {
		release()
		if queued != "" {
			waitForImportGroupTerminal(t, context.WithoutCancel(t.Context()), sourceDB, queued, "FAILED")
		}
	})
	result, err := service.QueueCreate(t.Context(), request)
	queued = result.JobID
	if hits != 1 || err == nil || result != (Created{}) {
		t.Fatalf("disabled target queued stale authority: result=%+v err=%v hits=%d", result, err, hits)
	}
	var enabled int
	if err := sourceDB.QueryRowContext(
		t.Context(),
		`SELECT enabled FROM platform_instances WHERE id=?`,
		request.TargetPlatformInstanceID,
	).Scan(
		&enabled,
	); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatal("target race never committed its competing change")
	}
	var count int
	if err := sourceDB.QueryRowContext(t.Context(), `SELECT count(*) FROM jobs WHERE kind='IMPORT_GROUP'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed admission retained %d jobs", count)
	}
}
