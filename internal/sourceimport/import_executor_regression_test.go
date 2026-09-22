package sourceimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

func TestImportExecutorStopsClaimingAfterOutcomeWriteFailure(t *testing.T) {
	t.Parallel()
	service, unit, _, _, _ := materialFixture(t)
	database := service.database
	mustExecSourceTest(t.Context(), t, database, `
DELETE FROM source_import_item_assets;
UPDATE source_import_items SET execution_state='PENDING' WHERE id='item';
UPDATE source_imports SET game_count=2,processable_item_count=2 WHERE id='import';
INSERT INTO source_import_items(id,import_id,collection_id,metadata_relative_path,game_ordinal,
source_key,title,discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,
created_at_ms,updated_at_ms)
SELECT 'second',import_id,collection_id,metadata_relative_path,1,
'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
'Second',discovery_state,'PENDING',metadata_json,source_manifest_json,source_manifest_digest,
created_at_ms,updated_at_ms FROM source_import_items WHERE id='item';
INSERT INTO source_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,
source_facts_digest,state,created_at_ms,updated_at_ms)
VALUES('second',0,'FILE','second.gba',4,
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','DISCOVERED',1,1);`)
	failure := errors.New("first item outcome unavailable")
	var failedWrites, laterClaims atomic.Int64
	service.database = testsupport.OpenSQLFaultDatabase(t, database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "existing_matches_json=COALESCE(?,existing_matches_json)") &&
				executorArgument(args, "item") && failedWrites.Add(1) == 1 {
				return failure
			}
			return nil
		},
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if strings.Contains(query, "execution_state='COPYING'") && executorArgument(args, "second") {
				count, err := result.RowsAffected()
				if err != nil {
					return result, err
				}
				laterClaims.Add(count)
			}
			return result, nil
		},
	})
	if err := service.importExecutor(service.roots["games"]).Execute(t.Context(), unit); !errors.Is(err, failure) {
		t.Fatalf("lost failed outcome cause: %v", err)
	}
	if failedWrites.Load() != 1 || laterClaims.Load() != 0 {
		t.Errorf("outcome failure writes=%d later claims=%d; execution must stop before its next item", failedWrites.Load(), laterClaims.Load())
	}
	var job, first, second string
	err := database.QueryRowContext(t.Context(), `SELECT jobs.state,first.execution_state,second.execution_state
FROM jobs JOIN source_import_items first ON first.id='item'
JOIN source_import_items second ON second.id='second' WHERE jobs.id='work'`).Scan(&job, &first, &second)
	if err != nil || job != "FAILED" || first != "COMMIT_FAILED" || second != "COMMIT_FAILED" {
		t.Errorf("owned execution failure was not settled: job=%s first=%s second=%s err=%v", job, first, second, err)
	}
}

func executorArgument(args []driver.NamedValue, value string) bool {
	for _, argument := range args {
		if argument.Value == value {
			return true
		}
	}
	return false
}
