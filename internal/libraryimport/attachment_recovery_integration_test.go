//go:build integration

package libraryimport

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	jobpersistence "retrom/internal/persistence/jobs"
	librarypersistence "retrom/internal/persistence/libraryimport"
	"retrom/internal/service/jobs"
	"retrom/internal/store"
	"retrom/internal/testsupport"
)

type attachmentRecoveryFixture struct {
	ctx                 context.Context
	database            *store.DB
	files               *filestore.Store
	jobID, itemID, kind string
}

func TestAttachmentRecoveryAndManualRetryRunFrozenInputToAcceptance(t *testing.T) {
	for _, kind := range []string{"REVIEW_ARCADE_PARENT_VALIDATE", "REVIEW_MULTI_DISC_VALIDATE"} {
		for _, manual := range []bool{false, true} {
			name := kind + "/recovery"
			if manual {
				name = kind + "/manual-retry"
			}
			t.Run(name, func(t *testing.T) {
				fixture := newAttachmentRecoveryFixture(t, kind)
				var clock atomic.Int64
				clock.Store(time.Now().UnixMilli() + 1000)
				now := func() time.Time { return time.UnixMilli(clock.Load()) }
				fixture.claimAndAbandon(t, now().UnixMilli())
				clock.Add(61_000)
				if manual {
					if _, err := fixture.database.SQL.ExecContext(t.Context(), `UPDATE jobs SET attempt_count=max_attempts WHERE id=?`, fixture.jobID); err != nil {
						t.Fatal(err)
					}
				}
				restarted := newTestImporter(t, fixture.database.SQL, fixture.files, testImportOptions{Now: now, MultiDiscEnabled: true})
				if err := restarted.RecoverAttachmentJobs(t.Context()); err != nil {
					t.Fatal(err)
				}
				var version int64
				if err := dbapi.QueryRowContext(t.Context(), fixture.database.SQL, `SELECT version FROM jobs WHERE id=?`, fixture.jobID).Scan(&version); err != nil {
					t.Fatal(err)
				}
				if manual {
					if _, err := jobs.New(jobpersistence.New(fixture.database.SQL), now).Retry(fixture.ctx, fixture.jobID, version); err != nil {
						t.Fatal(err)
					}
				} else {
					clock.Add(1000)
				}
				// No request wakeup is sent: startup must dispatch the durable queue.
				restarted.Start(fixture.ctx)
				waitParentJob(t, fixture.database.SQL, fixture.jobID, "SUCCEEDED")
				fixture.assertAcceptedOnce(t, manual)
			})
		}
	}
}

func newAttachmentRecoveryFixture(t *testing.T, kind string) attachmentRecoveryFixture {
	t.Helper()
	ctx, root, database, files, importer := newMultiDiscImportFixture(t)
	fixture := attachmentRecoveryFixture{ctx: ctx, database: database, files: files, kind: kind}
	if kind == "REVIEW_ARCADE_PARENT_VALIDATE" {
		insertArcadeParentCatalog(t, database.SQL)
		uploadID := completeMultiDiscUpload(t, ctx, database, files, root, "FILES", []multiDiscUploadFile{{path: "a.zip", contents: arcadeZIP(t, "a.bin", []byte("child"))}})
		created, err := importer.Create(ctx, CreateRequest{UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "arcade/fbneo"), MetadataProvider: "NONE"})
		if err != nil {
			t.Fatal(err)
		}
		itemID, version, snapshot, validation := reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
		uploadID = completeMultiDiscUpload(t, ctx, database, files, root, "FILES", []multiDiscUploadFile{{path: "b.zip", contents: arcadeZIP(t, "b.bin", []byte("parent"))}})
		var fileID string
		if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT id FROM upload_files WHERE upload_session_id=?`, uploadID).Scan(&fileID); err != nil {
			t.Fatal(err)
		}
		importer.Close() // Suppress the volatile wakeup while exercising durable admission.
		attached, err := importer.CreateArcadeParentAttachment(ctx, itemID, version, ParentAttachmentRequest{ValidationID: validation, BaseSourceSnapshotID: snapshot, DependencyMachine: "b", UploadFileID: fileID})
		if err != nil {
			t.Fatal(err)
		}
		fixture.itemID, fixture.jobID = itemID, attached.JobID
	} else {
		uploadID := completeMultiDiscDirectory(t, ctx, database, files, root, []multiDiscUploadFile{{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\n")}, {path: "game/one.chd", contents: fakeCHD("one")}})
		created, err := importer.Create(ctx, CreateRequest{UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "saturn/yabause"), MetadataProvider: "NONE", ContentMode: "MULTI_DISC"})
		if err != nil {
			t.Fatal(err)
		}
		itemID, version, _, _ := reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
		uploadID = completeMultiDiscUpload(t, ctx, database, files, root, "FILES", []multiDiscUploadFile{{path: "two.chd", contents: fakeCHD("two")}})
		importer.Close()
		attached, err := importer.CreateMultiDiscAttachment(ctx, itemID, version, MultiDiscAttachmentRequest{UploadID: uploadID})
		if err != nil {
			t.Fatal(err)
		}
		fixture.itemID, fixture.jobID = itemID, attached.JobID
	}
	return fixture
}

func (fixture attachmentRecoveryFixture) claimAndAbandon(t *testing.T, now int64) {
	t.Helper()
	if fixture.kind == "REVIEW_ARCADE_PARENT_VALIDATE" {
		if _, err := librarypersistence.NewArcadeParentAttachmentWorker(fixture.database.SQL).Claim(fixture.ctx, fixture.jobID, "dead-worker", now); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := librarypersistence.NewMultiDiscAttachmentWorker(fixture.database.SQL).Claim(fixture.ctx, fixture.jobID, "dead-worker", now); err != nil {
			t.Fatal(err)
		}
	}
}

func (fixture attachmentRecoveryFixture) assertAcceptedOnce(t *testing.T, manual bool) {
	t.Helper()
	var execution, attempt, snapshots, successes int64
	if err := dbapi.QueryRowContext(t.Context(), fixture.database.SQL, `SELECT execution_no,attempt_count,
(SELECT count(*) FROM import_item_source_snapshots WHERE import_item_id=?),
(SELECT count(*) FROM job_events WHERE job_id=? AND event_type='SUCCEEDED') FROM jobs WHERE id=?`, fixture.itemID, fixture.jobID, fixture.jobID).Scan(&execution, &attempt, &snapshots, &successes); err != nil {
		t.Fatal(err)
	}
	wantExecution, wantAttempt := int64(1), int64(2)
	if manual {
		wantExecution, wantAttempt = 2, 1
	}
	if execution != wantExecution || attempt != wantAttempt || snapshots != 2 || successes != 1 {
		t.Fatalf("execution=%d attempt=%d snapshots=%d successes=%d", execution, attempt, snapshots, successes)
	}
}
