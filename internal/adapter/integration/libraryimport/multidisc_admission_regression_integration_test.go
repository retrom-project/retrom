//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/testkit/testsupport"
)

type attachmentAdmissionCount struct {
	driver.Result
	cause error
}

func (result attachmentAdmissionCount) RowsAffected() (int64, error) { return 0, result.cause }

func TestMultiDiscAdmissionRejectsUnconfirmedRecords(t *testing.T) {
	t.Parallel()
	for _, point := range []struct {
		name, prefix, fragment string
		zero                   bool
	}{
		{"job zero", "INSERT INTO jobs", "REVIEW_MULTI_DISC_VALIDATE", true},
		{"input zero", "INSERT INTO job_input_snapshots", "VALUES", true},
		{"queue event zero", "INSERT INTO job_events", "QUEUED", true},
		{"draft count cause", "UPDATE review_drafts SET", "version=version+1", false},
	} {
		t.Run(point.name, func(t *testing.T) {
			t.Parallel()
			ctx, dir, db, blobs, importer := newMultiDiscImportFixture(t)
			upload := completeMultiDiscDirectory(t, ctx, db, blobs, dir, []multiDiscUploadFile{
				{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\n")},
				{path: "game/one.chd", contents: fakeCHD("one")},
			})
			created, err := importer.Create(ctx, CreateRequest{UploadID: upload, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, db.SQL, "saturn/yabause"), MetadataProvider: "NONE", ContentMode: "MULTI_DISC"})
			if err != nil {
				t.Fatal(err)
			}
			var itemID string
			if err := db.SQL.QueryRowContext(ctx, `SELECT id FROM import_items WHERE import_job_id=?`, created.ImportJobID).Scan(&itemID); err != nil {
				t.Fatal(err)
			}
			missing := completeMultiDiscUpload(t, ctx, db, blobs, dir, "FILES", []multiDiscUploadFile{{path: "two.chd", contents: fakeCHD("two")}})
			cause := errors.New("attachment admission count failed")
			if point.zero {
				cause = nil
			}
			var hits atomic.Int64
			fault := testsupport.OpenSQLFaultDatabase(t, db.SQL, testsupport.SQLFaultHooks{
				AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
					query = strings.Join(strings.Fields(query), " ")
					if strings.HasPrefix(query, point.prefix) && strings.Contains(query, point.fragment) {
						hits.Add(1)
						return attachmentAdmissionCount{Result: result, cause: cause}, nil
					}
					return result, nil
				},
			})
			_, err = New(fault, time.Now).WithBlobStore(blobs).CreateMultiDiscAttachment(ctx, itemID, 1, MultiDiscAttachmentRequest{UploadID: missing})
			if err == nil || cause != nil && !errors.Is(err, cause) || hits.Load() != 1 {
				t.Fatalf("unconfirmed admission: hits=%d err=%v", hits.Load(), err)
			}
			var version, jobs, attachments, events int
			readErr := db.SQL.QueryRowContext(ctx, `SELECT draft.version,
(SELECT count(*) FROM jobs WHERE kind='REVIEW_MULTI_DISC_VALIDATE' AND scope_id=draft.import_item_id),
(SELECT count(*) FROM review_multidisc_attachments WHERE import_item_id=draft.import_item_id),
(SELECT count(*) FROM review_events WHERE import_item_id=draft.import_item_id AND event_type='DISC_UPLOAD_REQUESTED')
FROM review_drafts draft WHERE import_item_id=?`, itemID).Scan(&version, &jobs, &attachments, &events)
			if readErr != nil || version != 1 || jobs != 0 || attachments != 0 || events != 0 {
				t.Fatalf("admission partially committed: version=%d jobs=%d attachments=%d events=%d err=%v", version, jobs, attachments, events, readErr)
			}
		})
	}
}
