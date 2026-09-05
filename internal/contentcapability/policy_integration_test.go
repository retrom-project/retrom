//go:build integration

package contentcapability_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/testsupport"
)

func TestBindingPolicyUsesTheConsumersTransactionSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(t.TempDir(), "policy.db"), func() time.Time {
		return time.UnixMilli(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	transaction, err := database.ReadOnly.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	query := `SELECT binding.binding_id,` + contentcapability.BindingPolicySQL + `
FROM runtime_target_bindings binding WHERE binding.core_id='yabause'`
	var bindingID string
	var before, sameSnapshot, after contentcapability.Policy
	if err := transaction.QueryRowContext(ctx, query).Scan(&bindingID, &before); err != nil {
		t.Fatal(err)
	}
	if !before.Supports(contentcapability.ModeMultiDisc) {
		t.Fatal("test did not select the actual Saturn binding")
	}
	if _, err := database.SQL.ExecContext(ctx, `
DELETE FROM runtime_binding_content_kinds WHERE binding_id=? AND content_kind='MULTI_DISC'`, bindingID); err != nil {
		t.Fatal(err)
	}
	if err := transaction.QueryRowContext(ctx, query).Scan(&bindingID, &sameSnapshot); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, query).Scan(&bindingID, &after); err != nil {
		t.Fatal(err)
	}
	if sameSnapshot.Digest() != before.Digest() || after.Supports(contentcapability.ModeMultiDisc) ||
		after.MultiDisc != nil || !after.Supports("SINGLE_FILE") {
		t.Fatal("policy escaped the transaction snapshot or retained stale derived capabilities")
	}
}

func TestMissingBindingScansAsNoCapabilities(t *testing.T) {
	t.Parallel()
	database, err := testsupport.OpenDatabase(context.Background(), filepath.Join(t.TempDir(), "missing.db"), func() time.Time {
		return time.UnixMilli(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	policy := contentcapability.NewPolicy(contentcapability.ModeMultiDisc)
	if err := database.SQL.QueryRow(`SELECT ` + contentcapability.BindingPolicySQL + `
FROM (SELECT 1) source LEFT JOIN runtime_target_bindings binding ON binding.binding_id='missing'`).Scan(&policy); err != nil {
		t.Fatal(err)
	}
	if len(policy.SupportedContentKinds) != 0 || policy.MultiDisc != nil || policy.Digest() != "" {
		t.Fatal("optional binding manufactured a capability")
	}
}
