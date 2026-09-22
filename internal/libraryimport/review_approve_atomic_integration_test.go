//go:build integration

package libraryimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"retrom/internal/cleanup"
	"retrom/internal/testsupport"
)

type approvalTransactionFault struct {
	stage                       string
	cause                       error
	games, items, sources, hits int64
}

func (fault *approvalTransactionFault) beforeExec(_ context.Context, query string, _ []driver.NamedValue) error {
	matched := fault.stage == "parent aggregate" && strings.Contains(query, "UPDATE import_jobs SET review_pending_item_count=")
	matched = matched || (fault.stage == "owner aggregate" && strings.HasPrefix(query, "UPDATE source_imports SET"))
	matched = matched || (fault.stage == "payload event" && strings.Contains(query, "INSERT INTO job_events"))
	if matched {
		fault.hits++
		return fault.cause
	}
	return nil
}

func (fault *approvalTransactionFault) afterExec(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(query, "INSERT INTO games("):
		fault.games += rows
	case strings.HasPrefix(query, "UPDATE import_items SET state='PUBLISHED'"):
		fault.items += rows
	case strings.HasPrefix(query, "UPDATE source_import_items SET execution_state="):
		fault.sources += rows
	}
	if fault.stage == "affected rows" && strings.Contains(query, "UPDATE import_jobs SET review_pending_item_count=") {
		fault.hits++
		return approvalResultFailure{Result: result, cause: fault.cause}, nil
	}
	return result, nil
}

type approvalResultFailure struct {
	driver.Result
	cause error
}

func (result approvalResultFailure) RowsAffected() (int64, error) { return 0, result.cause }

func TestApprovalLateFailureRollsBackPublicationAndSource(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"parent aggregate", "affected rows", "owner aggregate", "payload event"} {
		t.Run(stage, func(t *testing.T) { t.Parallel(); verifyApprovalLateFailure(t, stage) })
	}
}

func verifyApprovalLateFailure(t *testing.T, stage string) {
	t.Helper()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	itemID := created.Items[0].ItemID
	fixture.execute(t, `UPDATE source_import_items SET execution_state='REVIEW_PENDING',completed_at_ms=? WHERE id=?`, ownedSourceNow().UnixMilli(), request.Intent.ItemID)
	fixture.execute(t, `UPDATE source_imports SET review_pending_item_count=1 WHERE id=?`, request.Intent.ImportID)
	ctx := prepareApprovalSelections(t, fixture, itemID)
	before := approvalDatabaseRows(t, fixture.database)
	fault := &approvalTransactionFault{stage: stage, cause: errors.New("late approval store failure")}
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: fault.beforeExec, AfterExec: fault.afterExec,
	})
	result, err := fixture.service.Approve(ctx, itemID, 1)
	if !errors.Is(err, fault.cause) || result != (Approved{}) || fault.hits != 1 || fault.games != 1 {
		t.Fatalf("result=%+v err=%v fault=%+v", result, err, fault)
	}
	if fault.items != 1 {
		t.Fatalf("fault preceded real item write: %+v", fault)
	}
	if (stage == "owner aggregate" || stage == "payload event") && fault.sources != 1 {
		t.Fatalf("fault preceded real source write: %+v", fault)
	}
	assertApprovalRowsUnchanged(t, fixture.database, before)
	fixture.service.database = fixture.database
	approved, err := fixture.service.Approve(ctx, itemID, 1)
	if err != nil || approved.GameID == "" || approved.Status != "PUBLISHED" {
		t.Fatalf("retry=%+v err=%v", approved, err)
	}
	assertApprovalSourcePublishedOnce(t, fixture, itemID, request.Intent.ItemID, approved.GameID)
	assertApprovalSelectionsPublished(t, fixture, approved.GameID)
}

func assertApprovalSourcePublishedOnce(t *testing.T, fixture deduplicateFixture, itemID, sourceID, gameID string) {
	t.Helper()
	owner := captureDiscardOwner(t, fixture, sourceID)
	if owner.State != "PUBLISHED" || owner.Pending != 0 || owner.Published != 1 || owner.PayloadState != "RELEASING" || owner.PayloadJob == nil {
		t.Fatalf("owner=%+v", owner)
	}
	var actualGame, sourceKind string
	var games, events, variants int
	err := fixture.database.QueryRowContext(t.Context(), `SELECT source.published_game_id,game.content_source_kind,
 (SELECT count(*) FROM games),(SELECT count(*) FROM import_items WHERE id=? AND state='PUBLISHED'),
 (SELECT count(*) FROM game_variants WHERE game_id=game.id)
 FROM source_import_items source JOIN games game ON game.id=source.published_game_id WHERE source.id=?`, itemID, sourceID).
		Scan(&actualGame, &sourceKind, &games, &events, &variants)
	if err != nil {
		t.Fatal(err)
	}
	if actualGame != gameID || sourceKind != "IMPORT_RECEIVE" || games != 1 || events != 1 || variants != 1 {
		t.Fatalf("game=%s origin=%s games=%d events=%d variants=%d", actualGame, sourceKind, games, events, variants)
	}
}

func approvalDatabaseRows(t *testing.T, database *sql.DB) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{
		"games", "game_assets", "game_files", "game_variants", "variant_files", "variant_dependencies",
		"dos_entries", "game_tags", "tags", "content_identity_claims", "review_drafts", "review_uploaded_assets", "review_draft_tags",
		"import_items", "import_jobs", "source_import_items", "source_imports", "jobs", "job_events", "job_input_snapshots",
		"review_bulk_approvals", "review_bulk_approval_items", "blob_gc_candidates", "upload_files", "upload_sessions",
	} {
		result[table] = approvalTableRows(t, database, table)
	}
	return result
}

func approvalTableRows(t *testing.T, database *sql.DB, table string) string {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close approval columns", rows.Close()) }()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, `"`+name+`"`)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(columns) == 0 {
		t.Fatalf("unknown approval table %s", table)
	}
	var result string
	query := `SELECT COALESCE(json_group_array(row),'[]') FROM (SELECT json_array(` + strings.Join(columns, ",") + `) row FROM "` + table + `" ORDER BY 1)`
	if err := database.QueryRowContext(t.Context(), query).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertApprovalRowsUnchanged(t *testing.T, database *sql.DB, before map[string]string) {
	t.Helper()
	after := approvalDatabaseRows(t, database)
	if !reflect.DeepEqual(before, after) {
		for table, rows := range before {
			if rows != after[table] {
				t.Errorf("approval rollback changed table %s", table)
			}
		}
		t.FailNow()
	}
}
