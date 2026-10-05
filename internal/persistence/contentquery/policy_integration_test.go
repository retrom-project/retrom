//go:build integration

package contentquery_test

import (
	"context"
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	"retrom/internal/persistence/contentquery"

	dbapi "retrom/internal/database"

	"retrom/internal/cleanup"
	contentcapability "retrom/internal/content/capability"
	"retrom/internal/testsupport"
)

func TestBindingPolicyUsesTheConsumersTransactionSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := testsupport.OpenDatabase(ctx, testpostgres.DSN(t), func() time.Time {
		return time.UnixMilli(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	transaction, err := database.ReadOnly.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(transaction)
	query := `SELECT binding.binding_id,` + contentquery.BindingPolicySQL + `
FROM runtime_target_bindings binding WHERE binding.core_id='yabause'`
	var bindingID string
	var before, sameSnapshot, after contentcapability.Policy
	if err := dbapi.QueryRowContext(ctx, transaction, query).Scan(&bindingID, contentquery.ScanPolicy(&before)); err != nil {
		t.Fatal(err)
	}
	if !before.Supports(contentcapability.ModeMultiDisc) {
		t.Fatal("test did not select the actual Saturn binding")
	}
	if _, err := database.SQL.ExecContext(ctx, `
DELETE FROM runtime_binding_content_kinds WHERE binding_id=? AND content_kind='MULTI_DISC'`, bindingID); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(ctx, transaction, query).Scan(&bindingID, contentquery.ScanPolicy(&sameSnapshot)); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(ctx, database.SQL, query).Scan(&bindingID, contentquery.ScanPolicy(&after)); err != nil {
		t.Fatal(err)
	}
	if sameSnapshot.Digest() != before.Digest() || after.Supports(contentcapability.ModeMultiDisc) ||
		after.MultiDisc != nil || !after.Supports("SINGLE_FILE") {
		t.Fatal("policy escaped the transaction snapshot or retained stale derived capabilities")
	}
}

func TestMissingBindingScansAsNoCapabilities(t *testing.T) {
	t.Parallel()
	database, err := testsupport.OpenDatabase(context.Background(), testpostgres.DSN(t), func() time.Time {
		return time.UnixMilli(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	policy := contentcapability.NewPolicy(contentcapability.ModeMultiDisc)
	if err := dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT `+contentquery.BindingPolicySQL+`
FROM (SELECT 1) source LEFT JOIN runtime_target_bindings binding ON binding.binding_id='missing'`).Scan(contentquery.ScanPolicy(&policy)); err != nil {
		t.Fatal(err)
	}
	if len(policy.SupportedContentKinds) != 0 || policy.MultiDisc != nil || policy.Digest() != "" {
		t.Fatal("optional binding manufactured a capability")
	}
}

func TestPolicyFenceComparesJSONFactsAndDetectsChangedLimit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	database, err := testsupport.OpenDatabase(ctx, testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const maximum = int64(5 << 30)
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO runtime_target_input_limits(provider_id,target_id,role,max_file_bytes)
SELECT provider_id,target_id,'ROM',? FROM runtime_target_bindings WHERE core_id='yabause'
ON CONFLICT(provider_id,target_id,role) DO UPDATE SET max_file_bytes=excluded.max_file_bytes`, maximum); err != nil {
		t.Fatal(err)
	}
	query := `SELECT ` + contentquery.BindingPolicySQL + ` FROM runtime_target_bindings binding WHERE core_id='yabause'`
	var policy contentcapability.Policy
	if err := dbapi.QueryRowContext(ctx, database.SQL, query).Scan(contentquery.ScanPolicy(&policy)); err != nil {
		t.Fatal(err)
	}
	if policy.InputMaxFileBytes["ROM"] != maximum {
		t.Fatal("input limit did not retain its 64-bit value")
	}
	fence := `SELECT (` + contentquery.BindingPolicySQL + `)=? FROM runtime_target_bindings binding WHERE core_id='yabause'`
	var matches bool
	if err := dbapi.QueryRowContext(ctx, database.SQL, fence, contentquery.BindPolicy(policy)).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if !matches {
		t.Fatal("unchanged facts failed the fence after JSONB serialization")
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE runtime_target_input_limits SET max_file_bytes=? WHERE role='ROM'`, maximum-1); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(ctx, database.SQL, fence, contentquery.BindPolicy(policy)).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches {
		t.Fatal("changed input limit passed the stale policy fence")
	}
}
