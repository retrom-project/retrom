package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func metadataDatabase(t *testing.T) *sql.DB {
	t.Helper()
	owner, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "metadata.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	db := owner.SQL
	instance := testsupport.MustPlatformInstanceID(t, db, "gba/mgba")
	digest := strings.Repeat("a", 64)
	metadataExec(t, db, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile','Admin',1)`)
	metadataExec(t, db, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('actor','profile','admin','Admin','ADMIN','ENABLED',1,1)`)
	metadataExec(t, db, `INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('upload','COMPLETE','FILES',1,1,?,100,1,1)`, digest)
	metadataExec(t, db, `INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
SELECT 'import','upload',instance.id,instance.version,instance.platform_id,instance.default_core_id,binding.provider_id,binding.target_id,'NONE','{}',?,'REVIEW_PENDING',1,1,1,1
FROM platform_instances instance JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id WHERE instance.id=?`, digest, instance)
	metadataExec(t, db, `INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
VALUES('item','import',?,'REVIEW_PENDING','{}',?,'before',1,1)`, digest, digest)
	metadataExec(t, db, `INSERT INTO import_item_source_snapshots(id,import_item_id,source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES('snapshot','item','{}',?,'IDENTIFICATION',1)`, digest)
	metadataExec(t, db, `UPDATE import_items SET target_platform_instance_id=?,metadata_json='{"title":"Before"}',review_version=7,review_created_at_ms=1,review_updated_at_ms=1,effective_source_snapshot_id='snapshot' WHERE id='item'`, instance)
	return db
}

func metadataExec(t *testing.T, executor dbexec.Executor, query string, args ...any) {
	t.Helper()
	if _, err := executor.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func metadataNow() time.Time { return time.UnixMilli(10) }

type metadataState struct {
	JSON, Search, State       string
	Version, Updated, Changes int64
}

func readMetadataState(t *testing.T, db *sql.DB) metadataState {
	t.Helper()
	var result metadataState
	err := db.QueryRowContext(t.Context(), `SELECT metadata_json,search_text,state,review_version,review_updated_at_ms,review_version-7 FROM import_items WHERE id='item'`).Scan(&result.JSON, &result.Search, &result.State, &result.Version, &result.Updated, &result.Changes)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMetadataTransactionCommitsDraftAndSearchOnce(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	service := application.NewMetadataSeeder(NewMetadata(db), metadataNow)
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"})
	for range 2 {
		version, warnings, err := service.Seed(ctx, "item", application.ServerMetadata{Title: "新 Title"}, 2027)
		if err != nil || version != 8 || warnings == nil || len(warnings) != 0 {
			t.Fatalf("seed=%d %#v %v", version, warnings, err)
		}
	}
	result := readMetadataState(t, db)
	if result.Version != 8 || result.Search != "新 title" || result.Updated != 10 || result.Changes != 1 {
		t.Fatalf("projection=%#v", result)
	}
}

type metadataLateFailure struct {
	repository *Metadata
	cause      error
}

func (r metadataLateFailure) WithMetadata(ctx context.Context, work func(application.MetadataScope) error) error {
	return r.repository.WithMetadata(ctx, func(scope application.MetadataScope) error {
		if err := work(scope); err != nil {
			return err
		}
		return r.cause
	})
}

func TestMetadataTransactionRollsBackLateFailureWithoutSuccessResult(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	before := readMetadataState(t, db)
	cause := errors.New("handoff projection failed")
	service := application.NewMetadataSeeder(metadataLateFailure{NewMetadata(db), cause}, metadataNow)
	version, warnings, err := service.Seed(t.Context(), "item", application.ServerMetadata{Title: "Changed"}, 2027)
	if !errors.Is(err, cause) || version != 0 || warnings != nil {
		t.Fatalf("late failure result=%d %#v %v", version, warnings, err)
	}
	if after := readMetadataState(t, db); before != after {
		t.Fatalf("rollback changed metadata: before=%#v after=%#v", before, after)
	}
}

type metadataDrift struct {
	application.MetadataScope
	executor  dbexec.Executor
	statement string
}

func (scope metadataDrift) CurrentMetadata(ctx context.Context, id string) (application.MetadataDraft, error) {
	before, err := scope.MetadataScope.CurrentMetadata(ctx, id)
	if err != nil {
		return application.MetadataDraft{}, err
	}
	if _, err := scope.executor.ExecContext(ctx, scope.statement); err != nil {
		return application.MetadataDraft{}, err
	}
	return before, nil
}

func TestMetadataTransactionFencesVersionMetadataAndReviewState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, statement string }{
		{"version", `UPDATE import_items SET review_version=version+1 WHERE id='item'`},
		{"metadata", `UPDATE import_items SET metadata_json='{"title":"Other"}' WHERE id='item'`},
		{"state", `UPDATE import_items SET state='DISCARDED' WHERE id='item'`},
	} {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); assertMetadataFence(t, tc.statement) })
	}
}

func assertMetadataFence(t *testing.T, statement string) {
	t.Helper()
	db := metadataDatabase(t)
	before := readMetadataState(t, db)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	scope := metadataDrift{MetadataScope: BindMetadata(tx), executor: tx, statement: statement}
	version, _, err := application.NewMetadataSeeder(nil, metadataNow).SeedInScope(t.Context(), scope, "item", application.ServerMetadata{Title: "Changed"}, 2027)
	if !errors.Is(err, application.ErrVersionConflict) || version != 0 {
		t.Fatalf("stale metadata accepted: %d %v", version, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if after := readMetadataState(t, db); after != before {
		t.Fatalf("stale operation changed draft: %#v -> %#v", before, after)
	}
}

func TestMetadataTransactionRejectsMissingAndFinalizedItems(t *testing.T) {
	t.Parallel()
	db := metadataDatabase(t)
	service := application.NewMetadataSeeder(NewMetadata(db), metadataNow)
	for _, id := range []string{"missing", "item"} {
		if id == "item" {
			metadataExec(t, db, `UPDATE import_items SET state='DISCARDED' WHERE id='item'`)
		}
		version, _, err := service.Seed(t.Context(), id, application.ServerMetadata{Title: "Changed"}, 2027)
		if !errors.Is(err, application.ErrInvalid) || version != 0 {
			t.Fatalf("item %s result: %d %v", id, version, err)
		}
	}
}
