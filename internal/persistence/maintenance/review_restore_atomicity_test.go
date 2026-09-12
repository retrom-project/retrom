package maintenance

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/dbexec"
	application "retrom/internal/service/maintenance"
	"retrom/internal/testsupport"
)

func TestRestoredReviewFailureRollsBackSecurityAndHandoff(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"PEGASUS", "EMULATIONSTATION"} {
		for _, stage := range []string{"read", "metadata", "source", "affected", "progress", "aggregate", "audit"} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				t.Parallel()
				verifyRestoreReviewFailure(t, kind, stage)
			})
		}
	}
}

func restoreReviewFixture(t *testing.T, kind string) *sql.DB {
	t.Helper()
	var db *sql.DB
	if kind == "PEGASUS" {
		db, _ = restoredPegasusReview(t)
	} else {
		db, _ = restoredEmulationStationReview(t, false, false)
	}
	_, err := db.ExecContext(t.Context(), `INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,
created_at_ms,last_seen_at_ms,idle_expires_at_ms,absolute_expires_at_ms)
VALUES('restore-session','user',zeroblob(32),1,0,0,1000,10000)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func verifyRestoreReviewFailure(t *testing.T, kind, stage string) {
	t.Helper()
	db := restoreReviewFixture(t, kind)
	before := reviewRestoreSnapshot(t, db)
	cause := errors.New("restore handoff storage failed")
	var hits, drafts, sources atomic.Int64
	hooks := reviewRestoreFaultHooks(kind, stage, cause, &hits, &drafts, &sources)
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, hooks)
	err := runReviewRestoreTransaction(t.Context(), faultDB)
	if !errors.Is(err, cause) || hits.Load() != 1 {
		t.Fatalf("restore=%v fault hits=%d", err, hits.Load())
	}
	if stage != "read" && drafts.Load() != 1 {
		t.Fatalf("failure preceded real metadata write: %d", drafts.Load())
	}
	if restoreFaultFollowsSourceWrite(stage) && sources.Load() != 1 {
		t.Fatalf("failure preceded real source handoff: %d", sources.Load())
	}
	if after := reviewRestoreSnapshot(t, db); after != before {
		t.Fatal("restore retained partial security, metadata, ownership or source changes")
	}
	if err := runReviewRestoreTransaction(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var audits, events, pending int
	err = db.QueryRowContext(t.Context(), `SELECT
(SELECT count(*) FROM audit_events WHERE id='restore-audit'),
(SELECT count(*) FROM review_events WHERE import_item_id='handoff-item'),
(SELECT count(*) FROM import_items WHERE id='handoff-item' AND state='REVIEW_PENDING')`).Scan(&audits, &events, &pending)
	if err != nil || audits != 1 || events != 1 || pending != 1 {
		t.Fatalf("retry duplicated or lost result: audits=%d events=%d pending=%d error=%v", audits, events, pending, err)
	}
}

func restoreFaultFollowsSourceWrite(stage string) bool {
	return stage == "affected" || stage == "progress" || stage == "aggregate" || stage == "audit"
}

func reviewRestoreFaultHooks(
	kind, stage string, cause error, hits, drafts, sources *atomic.Int64,
) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if stage == "metadata" && matchesReviewRestoreWrite(kind, stage, query, args) {
				hits.Add(1)
				return cause
			}
			if stage == "read" && strings.HasPrefix(query, "SELECT '"+kind+"',source.id") {
				hits.Add(1)
				return cause
			}
			return nil
		},
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if stage != "affected" && matchesReviewRestoreWrite(kind, stage, query, args) {
				hits.Add(1)
				return cause
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.HasPrefix(query, "UPDATE review_drafts SET metadata_json=") {
				drafts.Add(1)
			}
			if matchesReviewRestoreWrite(kind, "source", query, args) {
				sources.Add(1)
				if stage == "affected" {
					hits.Add(1)
					return restoreAffectedFailure{Result: result, cause: cause}, nil
				}
			}
			return result, nil
		},
	}
}

type restoreAffectedFailure struct {
	driver.Result
	cause error
}

func (result restoreAffectedFailure) RowsAffected() (int64, error) { return 0, result.cause }

func matchesReviewRestoreWrite(kind, stage, query string, args []driver.NamedValue) bool {
	prefix, id := strings.ToLower(kind), "item"
	if kind == "EMULATIONSTATION" {
		id = "es-item"
	}
	statement, wanted := "", any(nil)
	switch stage {
	case "metadata":
		statement, wanted = "INSERT INTO review_events", "handoff-item"
	case "source", "affected":
		statement, wanted = "UPDATE "+prefix+"_import_items SET execution_state='REVIEW_PENDING'", id
	case "progress":
		statement, wanted = "INSERT INTO job_events", kind+"_IMPORT"
	case "aggregate":
		statement, wanted = "UPDATE "+prefix+"_imports SET state='FAILED'", int64(10)
	case "audit":
		statement, wanted = "INSERT INTO audit_events", "restore-audit"
	default:
		return false
	}
	if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), statement) {
		return false
	}
	for _, arg := range args {
		if arg.Value == wanted {
			return true
		}
	}
	return false
}

func runReviewRestoreTransaction(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer dbexec.Rollback(tx)
	records := writes{tx}
	if _, err := records.RevokeAccess(ctx, 10); err != nil {
		return err
	}
	if err := application.CompleteRestoredReviews(ctx, records.Reviews(), time.UnixMilli(10)); err != nil {
		return err
	}
	if _, err := records.StopExternalImports(ctx, 10); err != nil {
		return err
	}
	if err := records.StopBulkApprovals(ctx, 10); err != nil {
		return err
	}
	if err := records.Audit(ctx, application.FenceAudit{ID: "restore-audit", Now: 10}); err != nil {
		return err
	}
	return tx.Commit()
}

func reviewRestoreSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	snapshot := map[string][][]any{}
	for _, table := range []string{
		"auth_sessions", "account_links", "launch_sessions", "audit_events", "jobs",
		"job_events", "import_jobs", "import_items", "review_drafts", "review_events", "server_import_upload_owners",
		"pegasus_imports", "pegasus_import_items", "emulationstation_imports", "emulationstation_import_items",
	} {
		snapshot[table] = restoredTableRows(t, db, table)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
