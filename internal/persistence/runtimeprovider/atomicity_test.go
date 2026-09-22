package runtimeprovider

import (
	"strings"
	"testing"
	"time"

	service "retrom/internal/service/runtimeprovider"
)

func TestProviderActivationAndAuditCommitTogether(t *testing.T) {
	for _, failAudit := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "audit failure"}[failAudit], func(t *testing.T) {
			assertProviderActivationTransaction(t, failAudit)
		})
	}
}

func assertProviderActivationTransaction(t *testing.T, failAudit bool) {
	t.Helper()
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	activation := service.New(New(database.SQL))
	if err := activation.Reconcile(t.Context(), initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	if failAudit {
		if _, err := database.SQL.ExecContext(t.Context(), "DROP TABLE audit_events"); err != nil {
			t.Fatal(err)
		}
	}
	upgrade := projectionFixture("1.1.0", "b", []string{"state-v1", "state-v2"})
	err := activation.Reconcile(t.Context(), upgrade, time.UnixMilli(2))
	if (err != nil) != failAudit {
		t.Fatalf("audit failure=%v error=%v", failAudit, err)
	}
	var provider, digest string
	if err := database.SQL.QueryRowContext(t.Context(), `
SELECT (SELECT provider_version FROM runtime_providers WHERE provider_id='fixture'),
 (SELECT bundle_sha256 FROM runtime_providers WHERE provider_id='fixture')
`).Scan(&provider, &digest); err != nil {
		t.Fatal(err)
	}
	if failAudit {
		if provider != "1.0.0" || digest != strings.Repeat("a", 64) {
			t.Fatalf("failed activation leaked changes: %s %s", provider, digest)
		}
	} else if provider != "1.1.0" || digest != strings.Repeat("b", 64) {
		t.Fatalf("incomplete activation: %s %s", provider, digest)
	}
}
