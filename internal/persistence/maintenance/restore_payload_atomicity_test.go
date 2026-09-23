package maintenance

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

func TestRestoredPayloadSchedulingRollsBackSecurityAndReviewWrites(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"read", "job", "input", "event", "owner", "affected"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			verifyRestorePayloadFailure(t, stage)
		})
	}
}

func verifyRestorePayloadFailure(t *testing.T, stage string) {
	t.Helper()
	db := restorePayloadFixture(t)
	before := reviewRestoreSnapshot(t, db)
	cause := errors.New("payload schedule storage failed")
	fault := &restorePayloadFault{stage: stage, cause: cause}
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeQuery: fault.beforeQuery, BeforeExec: fault.beforeExec, AfterExec: fault.afterExec,
	})
	err := runReviewRestoreTransaction(t.Context(), faultDB)
	if !errors.Is(err, cause) || fault.hits.Load() != 1 || fault.drafts.Load() != 1 {
		t.Fatalf("payload failure lost cause or preceded review writes: %v hits=%d drafts=%d", err, fault.hits.Load(), fault.drafts.Load())
	}
	if stage == "affected" && fault.owners.Load() != 1 {
		t.Fatal("affected-row failure did not follow an actual owner update")
	}
	if after := reviewRestoreSnapshot(t, db); after != before {
		t.Fatal("payload failure retained partial security, review, source, job or input writes")
	}
	if err := runReviewRestoreTransaction(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var jobs, inputs, events int
	err = db.QueryRowContext(t.Context(), `SELECT count(*),
(SELECT count(*) FROM job_input_snapshots input JOIN jobs job ON input.job_id=job.id
 WHERE job.kind='PAYLOAD_RELEASE' AND job.scope_id='failed-source'),
(SELECT count(*) FROM job_events event JOIN jobs job ON event.job_id=job.id
 WHERE job.kind='PAYLOAD_RELEASE' AND job.scope_id='failed-source')
FROM jobs WHERE kind='PAYLOAD_RELEASE' AND scope_id='failed-source'`).Scan(&jobs, &inputs, &events)
	if err != nil || jobs != 1 || inputs != 1 || events != 1 {
		t.Fatalf("retry duplicated schedule: jobs=%d inputs=%d events=%d err=%v", jobs, inputs, events, err)
	}
}

func matchesRestoredPayloadWrite(stage, query string, args []driver.NamedValue, jobID string) bool {
	var prefix, id string
	switch stage {
	case "job":
		prefix, id = "INSERT INTO jobs", "SOURCE_IMPORT_ITEM"
	case "input":
		prefix, id = "INSERT INTO job_input_snapshots", jobID
	case "event":
		prefix, id = "INSERT INTO job_events", jobID
	case "owner":
		prefix, id = "UPDATE source_import_items SET payload_state='RELEASING'", "failed-source"
	default:
		return false
	}
	if id == "" || !strings.HasPrefix(strings.Join(strings.Fields(query), " "), prefix) {
		return false
	}
	for _, arg := range args {
		if arg.Value == id {
			return true
		}
	}
	return false
}

type restorePayloadFault struct {
	stage                string
	cause                error
	hits, drafts, owners atomic.Int64
	jobID                string
}

func (fault *restorePayloadFault) beforeQuery(_ context.Context, query string, args []driver.NamedValue) error {
	if fault.stage == "read" && query == "SELECT id FROM source_import_items WHERE payload_state='RETAINED' AND id>? ORDER BY id LIMIT ?" && len(args) == 2 && args[1].Value == int64(100) {
		fault.hits.Add(1)
		return fault.cause
	}
	return nil
}

func (fault *restorePayloadFault) beforeExec(_ context.Context, query string, args []driver.NamedValue) error {
	if matchesRestoredPayloadWrite(fault.stage, query, args, fault.jobID) {
		fault.hits.Add(1)
		return fault.cause
	}
	return nil
}

func (fault *restorePayloadFault) afterExec(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
	if strings.HasPrefix(query, "UPDATE import_items SET metadata_json=") {
		fault.drafts.Add(1)
	}
	if matchesRestoredPayloadWrite("job", query, args, "") {
		fault.jobID, _ = args[0].Value.(string)
	}
	if matchesRestoredPayloadWrite("owner", query, args, fault.jobID) {
		fault.owners.Add(1)
		if fault.stage == "affected" {
			fault.hits.Add(1)
			return restoreAffectedFailure{Result: result, cause: fault.cause}, nil
		}
	}
	return result, nil
}
