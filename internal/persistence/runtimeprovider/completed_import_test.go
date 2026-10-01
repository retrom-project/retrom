package runtimeprovider

import (
	"errors"
	"strings"
	"testing"
	"time"

	service "retrom/internal/service/runtimeprovider"
)

func TestTargetRemovalUsesActiveWorkAndIgnoresCompletedImportHistory(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "completed history"
		if active {
			name = "pending review"
		}
		t.Run(name, func(t *testing.T) {
			database := openProjectionDatabase(t)
			initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
			if err := service.New(New(database.SQL)).Reconcile(t.Context(), initial, time.UnixMilli(1)); err != nil {
				t.Fatal(err)
			}
			jobState, itemState := "COMPLETED", "PUBLISHED"
			pending, published := 0, 1
			if active {
				jobState, itemState, pending, published = "REVIEW_PENDING", "REVIEW_PENDING", 1, 0
			}
			statements := []struct {
				query string
				args  []any
			}{
				{`INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,enabled,version,created_at_ms,updated_at_ms)
 VALUES('history-directory','gbc','gambatte','History','history',1,1,1,1)`, nil},
				{`INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES('history-upload','COMPLETE','FILES',1,0,?,100,1,1)`, []any{strings.Repeat("a", 64)}},
				{
					`INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,
 default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,
 total_item_count,review_pending_item_count,published_item_count,created_at_ms,updated_at_ms)
 VALUES('history-import','history-upload','history-directory',1,'gbc','gambatte','fixture','target','NONE','{}',?,?,1,?,?,1,1)`,
					[]any{strings.Repeat("b", 64), jobState, pending, published},
				},
				{`INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
 VALUES('history-item','history-import',?,?,'{}',?,'history',1,1)`, []any{strings.Repeat("c", 64), itemState, strings.Repeat("d", 64)}},
			}
			for _, statement := range statements {
				if _, err := database.SQL.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
					t.Fatal(err)
				}
			}
			candidate := projectionFixtureForTarget("replacement", "1.1.0", "b", []string{"state-v1"})
			err := service.New(New(database.SQL)).Reconcile(t.Context(), candidate, time.UnixMilli(2))
			if active && !errors.Is(err, service.ErrProviderTargetReferenced) {
				t.Fatalf("active target removed: %v", err)
			}
			if !active && err != nil {
				t.Fatalf("completed task still pins removed target: %v", err)
			}
		})
	}
}
