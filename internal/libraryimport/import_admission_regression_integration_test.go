//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/dberrors"
	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func admissionFixture(t *testing.T) (*Service, CreateRequest) {
	t.Helper()
	database, blobs, dataDir := openImportGroupFixture(t, t.Context())
	uploadID := completeImportGroupUpload(t, t.Context(), database.SQL, blobs, dataDir, onsProjectArchive(t))
	service := newTestImporter(t, database.SQL, blobs, testImportOptions{Now: time.Now})
	return service, CreateRequest{
		UploadID:                 uploadID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "ons/onscripter_yuri"),
		MetadataProvider:         "NONE",
		ContentMode:              "ONS_PROJECT",
	}
}

func TestImportAdmissionPreservesReadFailure(t *testing.T) {
	t.Parallel()
	for _, match := range []string{"FROM upload_sessions", "FROM platform_instances pi", "FROM import_files f"} {
		t.Run(match, func(t *testing.T) {
			t.Parallel()
			service, request := admissionFixture(t)
			cause := errors.New("admission facts unavailable")
			hits := 0
			service = newTestImporter(t, testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if strings.Contains(query, match) {
						hits++
						return cause
					}
					return nil
				},
			}), service.blobs, testImportOptions{Now: service.now, MultiDiscEnabled: service.multiDiscImportEnabled})
			result, err := service.QueueCreate(t.Context(), request)
			if !errors.Is(err, cause) || errors.Is(err, ErrInvalid) || result != (Created{}) || hits != 1 {
				t.Fatalf("result=%+v err=%v hits=%d", result, err, hits)
			}
		})
	}
}

type admissionBeginDatabase struct {
	dbapi.DB
	beforeBegin func(context.Context) error
}

func (database admissionBeginDatabase) BeginTx(ctx context.Context, options *dbapi.TxOptions) (dbapi.Tx, error) {
	if err := database.beforeBegin(ctx); err != nil {
		return nil, err
	}
	return database.DB.BeginTx(ctx, options)
}

func TestImportAdmissionRejectsTargetDisabledBeforeQueueWrite(t *testing.T) {
	service, request := admissionFixture(t)
	sourceDB := service.database
	hits := 0
	// The competing change must commit before admission reserves the writer.
	database := admissionBeginDatabase{DB: sourceDB, beforeBegin: func(ctx context.Context) error {
		hits++
		_, err := sourceDB.ExecContext(ctx,
			`UPDATE platform_instances SET enabled=0,version=version+1 WHERE id=?`,
			request.TargetPlatformInstanceID)
		return err
	}}
	admissions := libraryservice.NewImportAdmissions(repository.NewImportAdmissions(database), nil, service.tags,
		libraryservice.ImportAdmissionOptions{Now: service.now})
	result, err := admissions.Queue(t.Context(), request)
	if hits != 1 || !errors.Is(err, ErrInvalid) || result != (Created{}) {
		t.Fatalf("disabled target queued stale authority: result=%+v err=%v hits=%d", result, err, hits)
	}
	var enabled, count int
	if err := dbapi.QueryRowContext(t.Context(), sourceDB,
		`SELECT enabled FROM platform_instances WHERE id=?`, request.TargetPlatformInstanceID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatal("target race never committed its competing change")
	}
	if err := dbapi.QueryRowContext(t.Context(), sourceDB,
		`SELECT count(*) FROM jobs WHERE kind='IMPORT_GROUP'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed admission retained %d jobs", count)
	}
}

func TestImportAdmissionBlocksTargetChangeUntilQueueCommit(t *testing.T) {
	service, request := admissionFixture(t)
	sourceDB := service.database
	if _, err := sourceDB.ExecContext(t.Context(), "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	hits := 0
	var competingError error
	disableTarget := func(ctx context.Context) error {
		_, err := sourceDB.ExecContext(ctx,
			`UPDATE platform_instances SET enabled=0,version=version+1 WHERE id=?`,
			request.TargetPlatformInstanceID)
		return err
	}
	faulty := testsupport.OpenSQLFaultDatabase(t, sourceDB, testsupport.SQLFaultHooks{
		BeforeExec: func(ctx context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "UPDATE upload_sessions SET version=version") {
				hits++
				competingError = disableTarget(ctx)
			}
			return nil
		},
	})
	admissions := libraryservice.NewImportAdmissions(repository.NewImportAdmissions(faulty), nil, service.tags,
		libraryservice.ImportAdmissionOptions{Now: service.now})
	result, err := admissions.Queue(t.Context(), request)
	if err != nil || result.JobID == "" || hits != 1 || dberrors.Classify(competingError) != "DATABASE_BUSY" {
		t.Fatalf("result=%+v err=%v competing=%v hits=%d", result, err, competingError, hits)
	}
	assertAdmittedImportRecords(t, sourceDB, result)
	var enabled int
	if err := dbapi.QueryRowContext(t.Context(), sourceDB,
		`SELECT enabled FROM platform_instances WHERE id=?`, request.TargetPlatformInstanceID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatal("competing write changed admission facts inside the transaction")
	}
	if err := disableTarget(t.Context()); err != nil {
		t.Fatalf("competing write could not proceed after admission committed: %v", err)
	}
	if err := dbapi.QueryRowContext(t.Context(), sourceDB,
		`SELECT enabled FROM platform_instances WHERE id=?`, request.TargetPlatformInstanceID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatal("competing write did not commit after admission released its lock")
	}
}
